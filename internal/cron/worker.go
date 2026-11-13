package cron

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"bangla-news-aggregator/internal/agent"
	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/imagestore"
	"bangla-news-aggregator/internal/repository"
	"bangla-news-aggregator/internal/rss"
)

var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

// These keys must match the usecase package's equivalents — kept as literals
// here (rather than importing usecase from cron) to avoid a needless
// cross-layer dependency for two string constants.
const (
	houseStyleSettingKey          = "ai_house_style"
	sensitiveCategoriesSettingKey = "ai_sensitive_categories"
	defaultSensitiveCategories    = "bangladesh,international"
)

// Worker runs the periodic feed-fetch → cluster → agentic rewrite → publish
// pipeline. Clustering (see cluster.go) is what lets it combine several
// publishers' independent coverage of the same event into one
// multi-source article instead of publishing near-duplicate stories.
type Worker struct {
	cfg          *config.Config
	feedRepo     repository.FeedRepository
	articleRepo  repository.ArticleRepository
	usageRepo    repository.AIUsageRepository
	settingsRepo repository.SettingsRepository
	seenURLRepo  repository.SeenURLRepository
	rssClient    *rss.Client
	aiClient     *ai.Client
	rewriter     *agent.Rewriter
	images       *imagestore.Store // nil (or cfg.ImageStorageDir == "") disables image caching
	gateway      *ai.Gateway
	journeyRepo  repository.JourneyRepository // optional; set via SetJourneyRepo to track the n8n pipeline
	popularRepo  repository.PopularRepository // optional; set via SetPopularRepo to read outlets' most-read lists
}

func NewWorker(
	cfg *config.Config,
	feedRepo repository.FeedRepository,
	articleRepo repository.ArticleRepository,
	usageRepo repository.AIUsageRepository,
	settingsRepo repository.SettingsRepository,
	seenURLRepo repository.SeenURLRepository,
) *Worker {
	aiClient := ai.NewClient(cfg)
	var images *imagestore.Store
	if cfg.ImageStorageDir != "" {
		images = imagestore.New(cfg.ImageStorageDir, "/img")
	}
	return &Worker{
		cfg:          cfg,
		feedRepo:     feedRepo,
		articleRepo:  articleRepo,
		usageRepo:    usageRepo,
		settingsRepo: settingsRepo,
		seenURLRepo:  seenURLRepo,
		rssClient:    rss.NewClient(),
		aiClient:     aiClient,
		rewriter:     agent.NewRewriter(aiClient),
		images:       images,
		gateway:      ai.NewGateway(cfg),
	}
}

// cacheImage tries to fetch and locally cache sourceImageURL, returning the
// local public path to store instead. On any failure (disabled, download
// error, size limit) it returns the original URL unchanged so the article
// still has *an* image rather than none.
func (w *Worker) cacheImage(ctx context.Context, sourceImageURL, logLabel string) string {
	if w.images == nil || sourceImageURL == "" {
		return sourceImageURL
	}
	localPath, err := w.images.Fetch(ctx, sourceImageURL)
	if err != nil {
		log.Printf("[cron] image cache miss for %q, keeping remote URL: %v", logLabel, err)
		return sourceImageURL
	}
	return localPath
}

// Start blocks and runs the pipeline immediately, then on every tick of
// cfg.CronInterval, until the context is cancelled.
func (w *Worker) Start(ctx context.Context) {
	log.Printf("[cron] worker started, interval=%s, workers=%d", w.cfg.CronInterval, w.cfg.FeedWorkerCount)

	// Anchor the recurring schedule to worker startup. Starting the ticker only
	// after RunOnce makes every slow fetch permanently push the next poll later.
	ticker := time.NewTicker(w.cfg.CronInterval)
	defer ticker.Stop()

	w.RunOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("[cron] worker stopped")
			return
		case <-ticker.C:
			w.RunOnce(ctx)
		}
	}
}

// RunOnce executes a single pass in four phases:
//  1. fetch every active feed in parallel and collect viable candidate items
//  2. drop anything already published (by URL — including secondary URLs of
//     a past multi-source synthesis)
//  3. cluster the remaining candidates by title similarity + publish-time
//     proximity, so independent coverage of the same event groups together
//  4. rewrite each cluster (single-source or multi-source synthesis) and save
func (w *Worker) RunOnce(ctx context.Context) {
	start := time.Now()
	log.Println("[cron] run started")

	feeds, err := w.feedRepo.ListActive()
	if err != nil {
		log.Printf("[cron] failed to load active feeds: %v", err)
		return
	}
	if len(feeds) == 0 {
		log.Println("[cron] no active feeds configured, skipping run")
		return
	}

	houseStyle, sensitiveCategories := w.loadRunSettings()
	budgetExceeded := w.dailyBudgetExceeded()
	if budgetExceeded {
		log.Printf("[cron] daily AI token budget (%d) reached — this run will fall back to unrewritten text and hold stories for review instead of calling any AI provider", w.cfg.AIDailyTokenBudget)
	}

	// ── Phase 1: fetch ──────────────────────────────────────────────
	candidates, fetchFailed := w.fetchAllFeeds(ctx, feeds)

	// ── Phase 2: dedupe by URL ──────────────────────────────────────
	candidates, dedupeSkipped, err := w.dropAlreadySeen(candidates)
	if err != nil {
		log.Printf("[cron] dedupe lookup failed, continuing with un-deduped candidates: %v", err)
	}

	if len(candidates) == 0 {
		log.Printf("[cron] run finished in %s: feeds=%d no new items (skipped=%d, fetch_failed=%d)",
			time.Since(start), len(feeds), dedupeSkipped, fetchFailed)
		return
	}

	// ── Phase 3: cluster ─────────────────────────────────────────────
	clusters := clusterCandidates(candidates)
	multiSourceClusters := 0
	for _, cl := range clusters {
		if len(cl.items) > 1 {
			multiSourceClusters++
		}
	}

	// ── Phase 4: rewrite + save ──────────────────────────────────────
	var saved, skipped, failed int
	for _, cl := range clusters {
		var s, sk, f int
		if len(cl.items) == 1 {
			s, sk, f = w.processSingle(ctx, cl.items[0], houseStyle, sensitiveCategories, budgetExceeded)
		} else {
			s, sk, f = w.processMulti(ctx, cl.items, houseStyle, sensitiveCategories, budgetExceeded)
		}
		saved += s
		skipped += sk
		failed += f
	}
	skipped += dedupeSkipped

	log.Printf(
		"[cron] run finished in %s: feeds=%d candidates=%d clusters=%d (multi_source=%d) saved=%d skipped=%d failed=%d fetch_failed=%d",
		time.Since(start), len(feeds), len(candidates), len(clusters), multiSourceClusters, saved, skipped, failed, fetchFailed,
	)
}

// loadRunSettings loads the admin-editable house style and sensitive-category
// list once per run rather than once per article/cluster — they rarely
// change mid-run, and this avoids hammering the DB.
func (w *Worker) loadRunSettings() (houseStyle string, sensitiveCategories map[string]bool) {
	houseStyle, ok, err := w.settingsRepo.Get(houseStyleSettingKey)
	if err != nil {
		log.Printf("[cron] failed to load house style, using default: %v", err)
	}
	if !ok || strings.TrimSpace(houseStyle) == "" {
		houseStyle = ai.DefaultHouseStyle
	}

	sensitiveValue, ok, err := w.settingsRepo.Get(sensitiveCategoriesSettingKey)
	if err != nil {
		log.Printf("[cron] failed to load sensitive-category list, using default: %v", err)
	}
	if !ok || strings.TrimSpace(sensitiveValue) == "" {
		sensitiveValue = defaultSensitiveCategories
	}
	sensitiveCategories = map[string]bool{}
	for _, c := range strings.Split(sensitiveValue, ",") {
		c = strings.ToLower(strings.TrimSpace(c))
		if c != "" {
			sensitiveCategories[c] = true
		}
	}
	return houseStyle, sensitiveCategories
}

// dailyBudgetExceeded reports whether today's (UTC) total AI token usage has
// already reached cfg.AIDailyTokenBudget. A budget of 0 means unlimited.
func (w *Worker) dailyBudgetExceeded() bool {
	if w.cfg.AIDailyTokenBudget <= 0 {
		return false
	}
	startOfDay := time.Now().UTC().Truncate(24 * time.Hour)
	used, err := w.usageRepo.TokensUsedSince(startOfDay)
	if err != nil {
		log.Printf("[cron] could not check daily AI token usage, assuming budget not exceeded: %v", err)
		return false
	}
	return used >= w.cfg.AIDailyTokenBudget
}

// fetchAllFeeds fetches every feed concurrently (bounded by FeedWorkerCount)
// and returns the flattened list of viable candidate items — link and title
// present, has an image, and enough visible text to be worth rewriting.
// These checks run before any AI call, same as before, just no longer tied
// to saving the item immediately afterward.
func (w *Worker) fetchAllFeeds(ctx context.Context, feeds []domain.FeedSource) (candidates []candidate, fetchFailed int) {
	feedCh := make(chan domain.FeedSource)
	var wg sync.WaitGroup
	var mu sync.Mutex

	workerCount := w.cfg.FeedWorkerCount
	if workerCount < 1 {
		workerCount = 1
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for feed := range feedCh {
				items, err := w.rssClient.Fetch(ctx, feed.URL)
				if recErr := w.feedRepo.RecordFetch(feed.ID, err); recErr != nil {
					log.Printf("[cron] could not record fetch status for feed %q: %v", feed.Name, recErr)
				}
				if err != nil {
					log.Printf("[cron] feed %q fetch failed: %v", feed.Name, err)
					mu.Lock()
					fetchFailed++
					mu.Unlock()
					continue
				}
				var viable []candidate
				for _, item := range items {
					if isViableCandidate(item) {
						viable = append(viable, candidate{item: item, feed: feed})
					}
				}
				mu.Lock()
				candidates = append(candidates, viable...)
				mu.Unlock()
			}
		}()
	}
	for _, f := range feeds {
		feedCh <- f
	}
	close(feedCh)
	wg.Wait()
	return candidates, fetchFailed
}

// minStoryRunes is the least visible text a story needs to count as a full
// article rather than an RSS teaser.
const minStoryRunes = 1000

func isViableCandidate(item rss.Item) bool {
	if item.Link == "" || strings.TrimSpace(item.Title) == "" {
		return false
	}
	visibleContent := html.UnescapeString(htmlTagRegex.ReplaceAllString(item.Content, " "))
	visibleRunes := utf8.RuneCountInString(strings.TrimSpace(visibleContent))
	if item.ImageURL == "" || visibleRunes < minStoryRunes {
		log.Printf("[cron] skipped incomplete story %q (body=%d chars, image=%t)", item.Title, visibleRunes, item.ImageURL != "")
		return false
	}
	return true
}

// dropAlreadySeen filters out candidates whose URL is already attached to a
// saved article — either as the primary Article.SourceURL (covers articles
// saved before this table existed, and ordinary single-source articles) or
// as any URL recorded in SeenSourceURL (covers the secondary URLs folded
// into a past multi-source synthesis, which never appear as a primary
// SourceURL anywhere).
func (w *Worker) dropAlreadySeen(candidates []candidate) ([]candidate, int, error) {
	if len(candidates) == 0 {
		return candidates, 0, nil
	}
	urls := make([]string, len(candidates))
	for i, c := range candidates {
		urls[i] = c.item.Link
	}

	unseenByArticles, err := w.articleRepo.FilterUnseenSourceURLs(urls)
	if err != nil {
		return candidates, 0, err
	}
	unseenFinal, err := w.seenURLRepo.FilterUnseen(unseenByArticles)
	if err != nil {
		return candidates, 0, err
	}
	stillNew := make(map[string]bool, len(unseenFinal))
	for _, u := range unseenFinal {
		stillNew[u] = true
	}

	out := make([]candidate, 0, len(candidates))
	skipped := 0
	for _, c := range candidates {
		if stillNew[c.item.Link] {
			out = append(out, c)
		} else {
			skipped++
		}
	}
	return out, skipped, nil
}

// reviewDecision captures why (if at all) an article should be held for
// human approval instead of auto-publishing — shared by processSingle and
// processMulti so the sensitive-category and fact-check rules are applied
// identically regardless of how many sources contributed.
type reviewDecision struct {
	status string
	reason string
}

func decideReview(category string, sensitiveCategories map[string]bool, factCheck agent.FactCheckResult, rewriteFailed bool) reviewDecision {
	var reasons []string
	if rewriteFailed {
		reasons = append(reasons, "AI rewrite ব্যর্থ হয়েছে, মূল লেখা অপরিবর্তিত প্রকাশিত হয়েছে")
	} else if !factCheck.Consistent {
		reasons = append(reasons, fmt.Sprintf(
			"মূল সংবাদের কিছু সংখ্যা/তথ্য (%s) পুনর্লিখিত সংস্করণে পাওয়া যায়নি — মিলিয়ে দেখুন",
			strings.Join(factCheck.MissingNumbers, ", "),
		))
	}
	if sensitiveCategories[category] {
		reasons = append(reasons, fmt.Sprintf("%q সংবেদনশীল বিভাগ হিসেবে চিহ্নিত — প্রকাশের আগে মানুষের অনুমোদন প্রয়োজন", category))
	}
	if len(reasons) == 0 {
		return reviewDecision{status: "published"}
	}
	return reviewDecision{status: "pending_review", reason: strings.Join(reasons, " | ")}
}

// processSingle rewrites and saves one candidate with no other coverage
// found this run — the original single-source flow, now also recording its
// URL in SeenSourceURL (not just as Article.SourceURL) so every future
// dedupe lookup goes through one consistent mechanism.
func (w *Worker) processSingle(ctx context.Context, c candidate, houseStyle string, sensitiveCategories map[string]bool, budgetExceeded bool) (saved, skipped, failed int) {
	item, feed := c.item, c.feed

	exists, err := w.articleRepo.ExistsByTitleAndDate(item.Title, item.Published)
	if err != nil {
		log.Printf("[cron] headline dedupe check failed for %q: %v", item.Title, err)
		return 0, 0, 1
	}
	if exists {
		return 0, 1, 0
	}

	var rewritten agent.Result
	if budgetExceeded {
		rewritten = agent.Result{Err: fmt.Errorf("daily AI token budget reached, skipped")}
	} else {
		rewritten = w.rewriter.Rewrite(ctx, houseStyle, item.Title, item.Content)
		for _, attempt := range rewritten.Attempts {
			w.logAIUsage(attempt)
		}
	}

	title, content, summary := item.Title, item.Content, ""
	if rewritten.Err != nil {
		log.Printf("[cron] agentic rewrite failed for %q, publishing original text: %v", item.Title, rewritten.Err)
		cleaned := strings.Join(strings.Fields(htmlTagRegex.ReplaceAllString(item.Content, " ")), " ")
		summary = truncate(cleaned, 280)
	} else {
		title, content, summary = rewritten.Title, rewritten.Body, rewritten.Summary
	}

	category := classifyItemCategory(item.Title, item.Categories, feed.Category)
	decision := decideReview(category, sensitiveCategories, rewritten.FactCheck, rewritten.Err != nil)

	sourceName := feed.Name
	if item.PublisherName != "" {
		sourceName = item.PublisherName
	}
	article := &domain.Article{
		Title:        title,
		Content:      content,
		Summary:      summary,
		SourceURL:    item.Link,
		SourceName:   sourceName,
		ImageURL:     w.cacheImage(ctx, item.ImageURL, item.Title),
		Category:     category,
		Tags:         strings.Join(rewritten.Tags, ", "),
		Status:       decision.status,
		ReviewReason: decision.reason,
		PublishedAt:  item.Published,
	}

	if err := w.articleRepo.Create(article); err != nil {
		if errors.Is(err, repository.ErrDuplicateArticle) {
			return 0, 1, 0
		}
		log.Printf("[cron] failed to save article %q: %v", item.Title, err)
		return 0, 0, 1
	}
	if err := w.seenURLRepo.RecordAll(article.ID, []string{item.Link}); err != nil {
		log.Printf("[cron] failed to record seen URL for %q: %v", item.Title, err)
	}

	return 1, 0, 0
}

// processMulti synthesizes a cluster of 2+ items — independent coverage of
// the same event from different feeds — into one multi-source article. This
// is the feature a plain per-feed summarizer can't do: it needs every
// source's text available at once.
func (w *Worker) processMulti(ctx context.Context, items []candidate, houseStyle string, sensitiveCategories map[string]bool, budgetExceeded bool) (saved, skipped, failed int) {
	// The richest item (most visible text) anchors the primary SourceURL,
	// category classification, and image — the others only contribute text
	// and a publisher-name credit.
	primary := items[0]
	for _, c := range items[1:] {
		if visibleLen(c.item.Content) > visibleLen(primary.item.Content) {
			primary = c
		}
	}

	sources := make([]ai.SynthesisSource, 0, len(items))
	names := make([]string, 0, len(items))
	urls := make([]string, 0, len(items))
	seenName := map[string]bool{}
	for _, c := range items {
		name := c.feed.Name
		if c.item.PublisherName != "" {
			name = c.item.PublisherName
		}
		sources = append(sources, ai.SynthesisSource{Title: c.item.Title, Content: c.item.Content, PublisherName: name})
		urls = append(urls, c.item.Link)
		if !seenName[name] {
			seenName[name] = true
			names = append(names, name)
		}
	}

	var rewritten agent.Result
	if budgetExceeded {
		rewritten = agent.Result{Err: fmt.Errorf("daily AI token budget reached, skipped")}
	} else {
		rewritten = w.rewriter.RewriteMulti(ctx, houseStyle, sources)
		for _, attempt := range rewritten.Attempts {
			w.logAIUsage(attempt)
		}
	}

	if rewritten.Err != nil {
		// Unlike a single-source failure, there is no safe single "original
		// text" to fall back to for several independently-worded sources —
		// publishing one source's raw text under a "multi-source" article
		// would misrepresent the others. Fall back to treating the primary
		// item alone as a normal single-source story instead of losing the
		// whole cluster; the other items stay candidates for a future run
		// (their URLs are not recorded as seen).
		log.Printf("[cron] synthesis unavailable for a %d-source cluster (seed %q), falling back to single-source for the primary item: %v",
			len(items), primary.item.Title, rewritten.Err)
		return w.processSingle(ctx, primary, houseStyle, sensitiveCategories, budgetExceeded)
	}

	category := classifyItemCategory(primary.item.Title, primary.item.Categories, primary.feed.Category)
	decision := decideReview(category, sensitiveCategories, rewritten.FactCheck, false)

	primaryName := primary.feed.Name
	if primary.item.PublisherName != "" {
		primaryName = primary.item.PublisherName
	}
	article := &domain.Article{
		Title:        rewritten.Title,
		Content:      rewritten.Body,
		Summary:      rewritten.Summary,
		SourceURL:    primary.item.Link,
		SourceName:   primaryName,
		Sources:      strings.Join(names, ", "),
		Tags:         strings.Join(rewritten.Tags, ", "),
		ImageURL:     w.cacheImage(ctx, primary.item.ImageURL, rewritten.Title),
		Category:     category,
		Status:       decision.status,
		ReviewReason: decision.reason,
		PublishedAt:  primary.item.Published,
	}

	if err := w.articleRepo.Create(article); err != nil {
		if errors.Is(err, repository.ErrDuplicateArticle) {
			return 0, 1, 0
		}
		log.Printf("[cron] failed to save synthesized article %q: %v", rewritten.Title, err)
		return 0, 0, 1
	}
	if err := w.seenURLRepo.RecordAll(article.ID, urls); err != nil {
		log.Printf("[cron] failed to record seen URLs for %q: %v", rewritten.Title, err)
	}

	log.Printf("[cron] synthesized %d-source article %q from: %s", len(items), rewritten.Title, strings.Join(names, ", "))
	return 1, 0, 0
}

func visibleLen(htmlContent string) int {
	return utf8.RuneCountInString(strings.TrimSpace(html.UnescapeString(htmlTagRegex.ReplaceAllString(htmlContent, " "))))
}

func (w *Worker) logAIUsage(r ai.Result) {
	usage := &domain.AIUsage{
		Provider:   r.Provider,
		Model:      r.Model,
		TokensUsed: r.TokensUsed,
		Success:    r.Err == nil && r.Text != "",
		DurationMs: r.Duration.Milliseconds(),
	}
	if r.Err != nil {
		usage.ErrorMsg = r.Err.Error()
	}
	if err := w.usageRepo.Create(usage); err != nil {
		log.Printf("[cron] failed to log ai usage: %v", err)
	}
}

func truncate(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "..."
}

var (
	sportsKeywords = []string{
		"ক্রিকেট", "ফুটবল", "খেলাধুলা", "ম্যাচ", "মেসি", "রোনালদো", "সাকিব", "তামিম", "শান্ত",
		"মুশফিক", "উইকেট", "রান", "সেঞ্চুরি", "ফিফটি", "গোল", "টাইব্রেকার", "ব্যালন ডি’অর",
		"বিপিএল", "আইপিএল", "বিশ্বকাপ", "সিরিজ", "টেস্ট", "ওয়ানডে", "টি-টোয়েন্টি", "টি২০",
		"অলরাউন্ডার", "বোলার", "ব্যাটার", "আম্পায়ার", "ফিফা", "আইসিসি", "লা লিগা",
		"চ্যাম্পিয়ন্স লিগ", "প্রিমিয়ার লিগ", "নেইমার", "রিয়াল মাদ্রিদ", "বার্সেলোনা",
	}

	businessKeywords = []string{
		"ব্যবসা", "বাণিজ্য", "অর্থনীতি", "ব্যাংক", "টাকা", "ডলার", "শেয়ারবাজার", "ডিএসই",
		"মুদ্রাস্ফীতি", "মূল্যস্ফীতি", "রাজস্ব", "বাজেট", "রপ্তানি", "আমদানি", "ঋণ", "সুদ",
		"জিডিপি", "পুঁজিবাজার", "বিনিয়োগ", "আইএমএফ", "বাণিজ্য ঘাটতি", "রেমিট্যান্স", "কাস্টমস",
		"ভ্যাট", "স্বর্ণের দাম", "তেলের দাম", "পে স্কেল", "গ্রেডের ক্রম", "বাড়ি ভাড়া", "শিল্প কারখানা",
	}

	techKeywords = []string{
		"প্রযুক্তি", "বিজ্ঞান", "স্মার্টফোন", "মোবাইল", "কম্পিউটার", "ল্যাপটপ", "ইন্টারনেট",
		"ওয়াই-ফাই", "সফটওয়্যার", "অ্যাপ", "গুগল", "ফেসবুক", "মেটা", "মাইক্রোসফট", "অ্যাপল",
		"এআই", "কৃত্রিম বুদ্ধিমত্তা", "সাইবার", "হ্যাক", "রোবট", "মহাকাশ", "নাসা", "ইলন মাস্ক",
		"সোশ্যাল মিডিয়া", "ইউটিউব", "ইনস্টাগ্রাম", "টুইটার", "চ্যাটজিপিটি", "টেলিকম", "ফোরজি", "ফাইভজি",
	}

	entertainmentKeywords = []string{
		"বিনোদন", "চলচ্চিত্র", "সিনেমা", "নাটক", "অভিনেতা", "অভিনেত্রী", "গায়ক", "গায়িকা",
		"নায়ক", "নায়িকা", "বলিউড", "হলিউড", "ঢালিউড", "গান", "মিউজিক", "শাকিব খান",
		"শাহরুখ", "সালমান", "আমির", "টাইগার শ্রফ", "অস্কার", "ফিল্ম", "কনসার্ট", "তারকা", "শুটিং",
		"পরিচালক", "সেলিব্রিটি", "হাল ফ্যাশন", "অপু বিশ্বাস", "বুবলী", "মেহজাবীন",
	}

	intlKeywords = []string{
		"আন্তর্জাতিক", "যুক্তরাষ্ট্র", "আমেরিকা", "ইসরায়েল", "ফিলিস্তিন", "গাজা", "লেবানন",
		"ইরান", "রাশিয়া", "ইউক্রেন", "পুতিন", "বাইডেন", "ট্রাম্প", "চীন", "ভারত", "পাকিস্তান",
		"জাতিসংঘ", "নেতানিয়াহু", "মধ্যপ্রাচ্য", "সিরিয়া", "হোয়াইট হাউস", "ওয়াশিংটন", "হিজবুল্লাহ",
		"হামাস", "ন্যাটো", "বেইজিং", "মস্কো", "তেহরান",
	}

	bangladeshKeywords = []string{
		"বাংলাদেশ", "ঢাকা", "চট্টগ্রাম", "সিলেট", "রাজশাহী", "খুলনা", "বরিশাল", "রংপুর",
		"ময়মনসিংহ", "উপদেষ্টা", "ইউনূস", "অন্তর্বর্তী", "নির্বাচন", "বিএনপি", "আওয়ামী লীগ",
		"জামায়াত", "ছাত্রদল", "শিবির", "বৈষম্যবিরোধী", "পুলিশ", "আদালত", "হাইকোর্ট",
		"আইনশৃঙ্খলা", "উপজেলা", "জেলা", "থানা", "গ্রেফতার", "মামলা", "সচিবালয়", "ঘূর্ণিঝড়",
		"সমুদ্রবন্দর", "কারাগার", "আসামি", "খালেদা জিয়া", "তারেক রহমান", "পল্টন", "সিপিবি",
	}
)

func countMatches(text string, keywords []string) int {
	score := 0
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		// These short words commonly occur inside unrelated Bengali words:
		// "খেলা" matches "খেলাপি" (loan defaulter), while "রান" matches "ইরান".
		if kw == "\u0996\u09c7\u09b2\u09be" || kw == "\u099f\u09be\u0995\u09be" || kw == "\u09aa\u09b0\u09bf\u099a\u09be\u09b2\u0995" {
			continue
		}
		// A passing mention of a social platform does not make a story technology news.
		if kw == "\u09ab\u09c7\u09b8\u09ac\u09c1\u0995" || kw == "\u099f\u09c1\u0987\u099f\u09be\u09b0" || kw == "\u0987\u09a8\u09b8\u09cd\u099f\u09be\u0997\u09cd\u09b0\u09be\u09ae" {
			continue
		}
		if matchesKeyword(lower, kw) {
			score++
		}
	}
	return score
}

func matchesKeyword(text, keyword string) bool {
	if strings.ContainsAny(keyword, " \t") {
		return strings.Contains(text, keyword)
	}
	for _, token := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsMark(r) && !unicode.IsNumber(r)
	}) {
		if token == keyword || strings.HasPrefix(token, keyword) {
			return true
		}
	}
	return false
}

// detectCategory inspects the article text and maps it into one of:
// "sports", "business", "technology", "entertainment", "international", "bangladesh"
func detectCategory(title, content, defaultCategory string) string {
	// Publisher pages often contain related-story lists and navigation text.
	// Scoring the body let those unrelated terms put stories in the wrong menu.
	// Headlines provide a much cleaner signal for category placement.
	text := title
	type catScore struct {
		name  string
		score int
	}

	scores := []catScore{
		{"sports", countMatches(text, sportsKeywords) * 3},
		{"business", countMatches(text, businessKeywords) * 2},
		{"technology", countMatches(text, techKeywords) * 3},
		{"entertainment", countMatches(text, entertainmentKeywords) * 3},
		{"international", countMatches(text, intlKeywords) * 2},
		{"bangladesh", countMatches(text, bangladeshKeywords) * 2},
	}

	bestCat := ""
	bestScore := 0
	for _, s := range scores {
		if s.score > bestScore {
			bestScore = s.score
			bestCat = s.name
		}
	}

	if bestScore >= 2 {
		return bestCat
	}

	defaultCategory = strings.ToLower(strings.TrimSpace(defaultCategory))
	switch defaultCategory {
	case "bangladesh", "international", "business", "sports", "technology", "entertainment", "general":
		return defaultCategory
	}
	return "general"
}

// ClassifyCategory exposes the same category rules to one-off data backfills.
func ClassifyCategory(title, content, defaultCategory string) string {
	return detectCategory(title, content, defaultCategory)
}

func classifyItemCategory(title string, feedCategories []string, defaultCategory string) string {
	hasBangladeshLabel := false
	for _, raw := range feedCategories {
		label := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case strings.Contains(label, "sport"), strings.Contains(label, "খেলা"), strings.Contains(label, "ক্রিকেট"), strings.Contains(label, "ফুটবল"):
			return "sports"
		case strings.Contains(label, "business"), strings.Contains(label, "economy"), strings.Contains(label, "ব্যবসা"), strings.Contains(label, "বাণিজ্য"), strings.Contains(label, "অর্থনীতি"):
			return "business"
		case strings.Contains(label, "technology"), strings.Contains(label, "tech"), strings.Contains(label, "প্রযুক্তি"), strings.Contains(label, "বিজ্ঞান"):
			return "technology"
		case strings.Contains(label, "entertainment"), strings.Contains(label, "বিনোদন"), strings.Contains(label, "বলিউড"), strings.Contains(label, "ঢালিউড"):
			return "entertainment"
		case strings.Contains(label, "international"), strings.Contains(label, "world"), strings.Contains(label, "আন্তর্জাতিক"), strings.Contains(label, "বিশ্ব"), strings.Contains(label, "ভারত"):
			return "international"
		case strings.Contains(label, "bangladesh"), strings.Contains(label, "বাংলাদেশ"), strings.Contains(label, "জাতীয়"), strings.Contains(label, "রাজনীতি"):
			hasBangladeshLabel = true
		}
	}
	category := detectCategory(title, "", defaultCategory)
	if category != "general" || !hasBangladeshLabel {
		return category
	}
	return "bangladesh"
}
