package cron

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"log"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"bangla-news-aggregator/internal/agent"
	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

// This file lets an external orchestrator (n8n) take over the AI rewrite
// step. The app keeps doing what it is good at — fetching feeds, scraping
// full article bodies, deduping, and applying the review rules — while the
// external tool only calls the LLM:
//
//	GET  /api/n8n/pending  -> PendingItems: new stories + ready-made prompts
//	POST /api/n8n/articles -> SaveExternal: the rewritten story comes back

// DraftJSONPlaceholder marks where the caller must splice the draft step's
// JSON output into ReviewPromptTemplate before running the review step.
const DraftJSONPlaceholder = "__DRAFT_JSON__"

// MinCorroboration is how many other outlets must carry a story before a
// sensitive-category story (e.g. bangladesh, international) may auto-publish.
const MinCorroboration = 2

var (
	ErrPendingBusy     = errors.New("another pending-items fetch is already running")
	ErrInvalidExternal = errors.New("invalid article")
)

// pendingMu stops overlapping n8n runs from scraping every feed twice at once.
var pendingMu sync.Mutex

// PendingItem is one not-yet-published story handed to the orchestrator.
type PendingItem struct {
	NewsID               string    `json:"news_id"`    // stable per source URL; key for the /api/n8n/journey row
	ForceReal            bool      `json:"force_real"` // admin overrode an earlier "fake" verdict — do not reject again
	HouseStyle           string    `json:"house_style"`
	SourceURL            string    `json:"source_url"`
	RelatedURLs          []string  `json:"related_urls,omitempty"` // other feeds' copies of the same story; send back so they are marked seen too
	Title                string    `json:"title"`
	Content              string    `json:"content"`
	ImageURL             string    `json:"image_url"`
	SourceName           string    `json:"source_name"`
	Category             string    `json:"category"`
	PublishedAt          time.Time `json:"published_at"`
	SystemPrompt         string    `json:"system_prompt"`
	DraftPrompt          string    `json:"draft_prompt"`
	ReviewPromptTemplate string    `json:"review_prompt_template"`
}

// PendingItems fetches every active feed and returns up to limit new
// stories, newest first, each with the same prompts the built-in agent uses
// (including the admin-editable house style). Nothing is saved or reserved:
// a story only stops being "pending" once SaveExternal stores it.
func (w *Worker) PendingItems(ctx context.Context, limit int) ([]PendingItem, error) {
	if !pendingMu.TryLock() {
		return nil, ErrPendingBusy
	}
	defer pendingMu.Unlock()

	feeds, err := w.feedRepo.ListActive()
	if err != nil {
		return nil, fmt.Errorf("load active feeds: %w", err)
	}
	if len(feeds) == 0 {
		return []PendingItem{}, nil
	}
	houseStyle, _ := w.loadRunSettings()
	system := ai.RewriteSystemPrompt(houseStyle)

	candidates, _ := w.fetchAllFeeds(ctx, feeds)
	candidates, _, err = w.dropAlreadySeen(candidates)
	if err != nil {
		return nil, fmt.Errorf("dedupe lookup: %w", err)
	}

	// Same-story copies from different feeds collapse into one item; the
	// extra URLs ride along so they get marked seen when the story is saved.
	clusters := clusterCandidates(candidates)
	sort.SliceStable(clusters, func(i, j int) bool {
		return clusters[i].items[0].item.Published.After(clusters[j].items[0].item.Published)
	})

	// Stories flagged as fake (awaiting an admin) or rejected stay out of the
	// queue until an admin re-queues them from /admin/journey.
	journeys := map[string]domain.NewsJourney{}
	if w.journeyRepo != nil {
		urls := make([]string, 0, len(clusters))
		for _, cl := range clusters {
			urls = append(urls, cl.items[0].item.Link)
		}
		if journeys, err = w.journeyRepo.Lookup(urls); err != nil {
			return nil, fmt.Errorf("journey lookup: %w", err)
		}
	}

	out := make([]PendingItem, 0, limit)
	for _, cl := range clusters {
		if len(out) >= limit {
			break
		}
		item, feed := cl.items[0].item, cl.items[0].feed
		// Aggregator feeds (e.g. Google News) append " - Publisher" to titles;
		// the portal never shows where a story came from.
		for _, name := range []string{item.PublisherName, feed.Name} {
			if name != "" && strings.HasSuffix(item.Title, " - "+name) {
				item.Title = strings.TrimSpace(strings.TrimSuffix(item.Title, " - "+name))
			}
		}
		j, tracked := journeys[item.Link]
		if tracked && (j.PublishedStatus == "rejected" || j.PublishedStatus == "suspected_fake" || j.PublishedStatus == "needs_editor") {
			continue
		}
		exists, err := w.articleRepo.ExistsByTitleAndDate(item.Title, item.Published)
		if err != nil {
			return nil, fmt.Errorf("headline dedupe check: %w", err)
		}
		if exists {
			continue
		}
		// Only stories with a photo we can actually serve make it to the
		// portal: download it now, and skip the story if that fails (blocked
		// hotlink, dead link, or an icon instead of a photo).
		imageURL := item.ImageURL
		if w.images != nil {
			local, err := w.images.Fetch(ctx, item.ImageURL)
			if err != nil {
				log.Printf("[n8n] skipping %q: no usable photo: %v", item.Title, err)
				continue
			}
			imageURL = local
		}
		var related []string
		for _, other := range cl.items[1:] {
			related = append(related, other.item.Link)
		}
		sourceName := feed.Name
		if item.PublisherName != "" {
			sourceName = item.PublisherName
		}
		out = append(out, PendingItem{
			NewsID:               NewsIDFor(item.Link),
			ForceReal:            tracked && j.ForceReal,
			HouseStyle:           houseStyle,
			SourceURL:            item.Link,
			RelatedURLs:          related,
			Title:                item.Title,
			Content:              item.Content,
			ImageURL:             imageURL,
			SourceName:           sourceName,
			Category:             classifyItemCategory(item.Title, item.Categories, feed.Category),
			PublishedAt:          item.Published,
			SystemPrompt:         system,
			DraftPrompt:          ai.BuildRewriteDraftPrompt(item.Title, item.Content),
			ReviewPromptTemplate: ai.BuildRewriteReviewPrompt(item.Title, item.Content, DraftJSONPlaceholder),
		})
	}
	return w.fullStories(ctx, out), nil
}

// fullStories makes sure every story handed on is the whole story, taken
// from its own source page. Each page is read again; its text replaces the
// feed's when it is clearly longer and contains the feed's opening (so it is
// the same story, not a page of teasers). A story whose text still looks cut
// off ("…", "বিস্তারিত পড়ুন") is left out.
func (w *Worker) fullStories(ctx context.Context, items []PendingItem) []PendingItem {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range items {
		wg.Add(1)
		go func(it *PendingItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_, _, page := w.rssClient.Enrich(ctx, it.SourceURL)
			if visibleLen(page) > visibleLen(it.Content)*115/100 && sameOpening(it.Content, page) {
				log.Printf("[desk] %q: using the source page's full text (%d chars instead of %d)", it.Title, visibleLen(page), visibleLen(it.Content))
				it.Content = page
			}
		}(&items[i])
	}
	wg.Wait()

	out := items[:0]
	for _, it := range items {
		if looksCutOff(it.Content) {
			log.Printf("[desk] skipping %q: only part of the story is available", it.Title)
			continue
		}
		it.DraftPrompt = ai.BuildRewriteDraftPrompt(it.Title, it.Content)
		it.ReviewPromptTemplate = ai.BuildRewriteReviewPrompt(it.Title, it.Content, DraftJSONPlaceholder)
		out = append(out, it)
	}
	return out
}

func plainWords(htmlContent string) string {
	return strings.Join(strings.Fields(html.UnescapeString(htmlTagRegex.ReplaceAllString(htmlContent, " "))), " ")
}

// sameOpening reports whether the page text contains the feed text's first words.
func sameOpening(feed, page string) bool {
	f := []rune(plainWords(feed))
	if len(f) > 60 {
		f = f[:60]
	}
	return len(f) >= 20 && strings.Contains(plainWords(page), string(f))
}

// looksCutOff spots feed teasers: text that ends in an ellipsis or a
// "read more" link instead of a sentence.
func looksCutOff(htmlContent string) bool {
	t := strings.TrimSpace(plainWords(htmlContent))
	t = strings.TrimSpace(strings.TrimRight(t, "]) "))
	for _, end := range []string{"...", "…", "[…", "[...", "বিস্তারিত", "বিস্তারিত পড়ুন", "আরও পড়ুন", "বাকি অংশ পড়ুন", "Read more", "Continue reading"} {
		if strings.HasSuffix(t, end) {
			return true
		}
	}
	return false
}

// NewsIDFor derives a stable id from a story's source URL, so a retry of the
// same story in a later run updates the same journey row instead of adding one.
func NewsIDFor(sourceURL string) string {
	sum := sha256.Sum256([]byte(sourceURL))
	return hex.EncodeToString(sum[:10])
}

// ExternalArticle is a story rewritten outside the app (by n8n).
type ExternalArticle struct {
	NewsID      string    `json:"news_id"` // optional; when set, the matching journey row is marked published
	SourceURL   string    `json:"source_url"`
	RelatedURLs []string  `json:"related_urls"`
	Title       string    `json:"title"`
	Body        string    `json:"body"` // HTML paragraphs
	Summary     string    `json:"summary"`
	Tags        []string  `json:"tags"`
	ImageURL    string    `json:"image_url"`
	SourceName  string    `json:"source_name"`
	Category    string    `json:"category"`
	PublishedAt time.Time `json:"published_at"`

	// The untouched source text, used for the same number-consistency fact
	// check the built-in agent runs. Optional, but without it that check is skipped.
	OriginalTitle   string `json:"original_title"`
	OriginalContent string `json:"original_content"`

	// ForceReview holds the story for an editor with this reason, even if
	// the review rules would have published it (the agent chose to hold).
	ForceReview string `json:"force_review"`

	// Corroborated is how many other outlets were seen (by code, in search
	// results or our own feeds) carrying the same story. At
	// MinCorroboration or more, sensitive categories may publish without an editor.
	Corroborated int `json:"corroborated"`

	// Optional AI usage accounting, shown on /admin/ai-usage.
	AIProvider string `json:"ai_provider"`
	AIModel    string `json:"ai_model"`
	TokensUsed int    `json:"tokens_used"`
}

// SaveExternal validates and stores an externally rewritten story, applying
// the same sensitive-category and fact-check review rules as the built-in
// agent. Returns repository.ErrDuplicateArticle if the story already exists.
func (w *Worker) SaveExternal(ctx context.Context, in ExternalArticle) (*domain.Article, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(in.Body)
	if u, err := url.Parse(in.SourceURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: source_url must be an http(s) URL", ErrInvalidExternal)
	}
	if in.Title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidExternal)
	}
	if n := visibleLen(in.Body); n < 200 {
		return nil, fmt.Errorf("%w: body too short (%d visible chars, need 200)", ErrInvalidExternal, n)
	}
	in.ImageURL = strings.TrimSpace(in.ImageURL)
	if in.ImageURL == "" {
		return nil, fmt.Errorf("%w: image_url is required (stories without a photo are not published)", ErrInvalidExternal)
	}
	if in.PublishedAt.IsZero() {
		in.PublishedAt = time.Now()
	}
	category := strings.ToLower(strings.TrimSpace(in.Category))
	if category == "" {
		category = "general"
	}

	dedupeTitle := in.OriginalTitle
	if strings.TrimSpace(dedupeTitle) == "" {
		dedupeTitle = in.Title
	}
	exists, err := w.articleRepo.ExistsByTitleAndDate(dedupeTitle, in.PublishedAt)
	if err != nil {
		return nil, fmt.Errorf("headline dedupe check: %w", err)
	}
	if exists {
		w.finishJourney(in.NewsID, map[string]any{"published_status": "duplicate"})
		return nil, repository.ErrDuplicateArticle
	}

	factCheck := agent.FactCheckResult{Consistent: true}
	if strings.TrimSpace(in.OriginalContent) != "" {
		factCheck = agent.CheckFactConsistency([]string{in.OriginalTitle, in.OriginalContent}, in.Body)
	}
	_, sensitive := w.loadRunSettings()
	if in.Corroborated >= MinCorroboration && sensitive[category] {
		// Owner's policy: a sensitive-category story confirmed by enough
		// independent outlets publishes on its own (other rules still apply).
		relaxed := make(map[string]bool, len(sensitive))
		for k, v := range sensitive {
			relaxed[k] = v
		}
		delete(relaxed, category)
		sensitive = relaxed
	}
	decision := decideReview(category, sensitive, factCheck, false)
	if note := strings.TrimSpace(in.ForceReview); note != "" {
		decision.status = "pending_review"
		decision.reason = strings.TrimPrefix(decision.reason+" | "+note, " | ")
	}

	summary := strings.TrimSpace(in.Summary)
	if summary == "" {
		summary = truncate(strings.Join(strings.Fields(htmlTagRegex.ReplaceAllString(in.Body, " ")), " "), 280)
	}

	// The portal lists news newest first by this time. A story that goes live
	// now must show as new, not hours down the page at its source's time.
	// (A held story gets its time when an editor approves it.)
	publishedAt := in.PublishedAt
	if decision.status == "published" {
		publishedAt = time.Now()
	}

	article := &domain.Article{
		Title:        in.Title,
		Content:      in.Body,
		Summary:      summary,
		SourceURL:    in.SourceURL,
		SourceName:   in.SourceName,
		ImageURL:     w.externalImage(ctx, in.ImageURL, in.Title),
		Category:     category,
		Tags:         strings.Join(cleanExternalTags(in.Tags), ", "),
		Status:       decision.status,
		ReviewReason: decision.reason,
		PublishedAt:  publishedAt,
	}
	if err := w.articleRepo.Create(article); err != nil {
		if errors.Is(err, repository.ErrDuplicateArticle) {
			w.finishJourney(in.NewsID, map[string]any{"published_status": "duplicate"})
		}
		return nil, err
	}
	now := time.Now()
	w.finishJourney(in.NewsID, map[string]any{
		"published_status": article.Status, // "published", or "pending_review" for sensitive categories / fact-check misses
		"published_at":     &now,
		"article_id":       &article.ID,
	})

	if err := w.seenURLRepo.RecordAll(article.ID, append([]string{in.SourceURL}, in.RelatedURLs...)); err != nil {
		// Not fatal: the primary URL is already unique on the article itself.
		log.Printf("[n8n] failed to record seen URLs for %q: %v", in.Title, err)
	}
	if in.AIProvider != "" {
		articleID := article.ID
		usage := &domain.AIUsage{
			Provider:   "n8n:" + in.AIProvider,
			Model:      in.AIModel,
			ArticleID:  &articleID,
			TokensUsed: in.TokensUsed,
			Success:    true,
		}
		if err := w.usageRepo.Create(usage); err != nil {
			log.Printf("[n8n] failed to log ai usage for %q: %v", in.Title, err)
		}
	}
	return article, nil
}

func cleanExternalTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(strings.ReplaceAll(t, ",", " "))
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

// externalImage keeps a photo PendingItems already cached locally as-is, and
// caches anything else (an older workflow sending the remote URL).
func (w *Worker) externalImage(ctx context.Context, imageURL, logLabel string) string {
	if w.images != nil && strings.HasPrefix(imageURL, w.images.PublicPrefix+"/") {
		return imageURL
	}
	return w.cacheImage(ctx, imageURL, logLabel)
}

// AIComplete runs one n8n AI step through the provider gateway and logs
// every attempt (successful or not) on /admin/ai-usage.
func (w *Worker) AIComplete(ctx context.Context, req ai.GatewayRequest) (ai.Result, []ai.Result) {
	res, attempts := w.gateway.Run(ctx, req)
	for _, a := range attempts {
		a.Provider = "n8n:" + a.Provider
		w.logAIUsage(a)
	}
	return res, attempts
}
