package english

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/imagestore"
	"bangla-news-aggregator/internal/rss"
)

// Source is one English outlet's feed. Addresses with "sitemap" are read as
// news sitemaps.
type Source struct{ Name, Feed string }

// sources are Bangladesh's English-language outlets that publish a feed the
// portal may read. Daily Sun, Jagonews24 English and bdnews24 answer bots
// with a block page and are left out; UNB, The Independent, Bangladesh Post
// and Dhaka Post English publish no feed; The Financial Express and BSS list
// only their section pages.
var sources = []Source{
	{"The Daily Star", "https://www.thedailystar.net/frontpage/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/news/bangladesh/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/news/politics/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/news/crime-justice/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/news/accidents-and-fires/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/news/world/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/business/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/sports/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/opinion/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/opinion/editorial/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/life-living/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/entertainment/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/culture/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/tech-startup/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/environment/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/health/rss.xml"},
	{"The Daily Star", "https://www.thedailystar.net/youth/rss.xml"},
	{"Dhaka Tribune", "https://www.dhakatribune.com/feed/"},
	{"Prothom Alo English", "https://en.prothomalo.com/feed/"},
	{"Prothom Alo English", "https://en.prothomalo.com/stories.rss"},
	{"The Business Standard", "https://www.tbsnews.net/top-news/rss.xml"},
	{"Daily Observer", "https://www.observerbd.com/rss.php"},
	{"New Age", "https://www.newagebd.net/news-sitemap/1.xml"},
}

const (
	perRunNew       = 40 // new stories looked at per run
	perSourcePerRun = 5  // per feed, so one busy feed cannot fill the page
	maxAge          = 96 * time.Hour
)

type worker struct {
	db       *gorm.DB
	rss      *rss.Client
	images   *imagestore.Store
	ai       *ai.Gateway
	rewrites int             // a day's cap on AI rewrites, shared with nothing else
	failed   map[string]bool // e-paper stories whose full text could not be had
	tries    map[string]int  // failed rewrites of an e-paper story
	mirrored bool            // the e-paper is copied from the hosted site (EN_MIRROR): lay out none here
}

// run reads every feed once and stores the new stories.
func (w *worker) run(ctx context.Context) {
	var found []rss.Item
	names := map[string]string{}
	feeds := map[string]string{}
	for _, src := range sources {
		items, err := w.rss.Fetch(ctx, src.Feed)
		if err != nil {
			log.Printf("[en] %s: %v", src.Name, err)
			continue
		}
		n := 0
		for _, it := range items {
			if it.Link == "" || n >= perSourcePerRun*3 {
				continue
			}
			if !it.Published.IsZero() && time.Since(it.Published) > maxAge {
				continue
			}
			names[it.Link] = src.Name
			feeds[it.Link] = src.Feed
			found = append(found, it)
			n++
		}
	}
	links := make([]string, 0, len(found))
	for _, it := range found {
		links = append(links, it.Link)
	}
	seen := w.seen(links)

	perSource := map[string]int{}
	added := 0
	for _, it := range found {
		if added >= perRunNew || ctx.Err() != nil {
			break
		}
		name, feed := names[it.Link], feeds[it.Link]
		if seen[it.Link] || perSource[feed] >= perSourcePerRun {
			continue
		}
		seen[it.Link] = true
		w.db.Create(&Seen{URL: it.Link})
		if w.repeated(plain(it.Title)) {
			continue // the same story under another address
		}
		if a, ok := w.build(ctx, it, name); ok {
			if err := w.db.Create(&a).Error; err == nil {
				perSource[feed]++
				added++
			}
		}
	}
	if added > 0 {
		log.Printf("[en] added %d stories", added)
	}
}

// repeated reports a headline already stored in the last three days.
func (w *worker) repeated(title string) bool {
	if title == "" {
		return false
	}
	var n int64
	w.db.Model(&Article{}).Where("title = ? AND created_at > ?", title, time.Now().Add(-72*time.Hour)).Count(&n)
	return n > 0
}

func (w *worker) seen(links []string) map[string]bool {
	out := map[string]bool{}
	if len(links) == 0 {
		return out
	}
	var rows []Seen
	w.db.Where("url IN ?", links).Find(&rows)
	for _, r := range rows {
		out[r.URL] = true
	}
	var known []string
	w.db.Model(&Article{}).Where("source_url IN ?", links).Pluck("source_url", &known)
	for _, u := range known {
		out[u] = true
	}
	return out
}

var tagRe = regexp.MustCompile(`<[^>]+>`)

func plain(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

// build turns a feed entry into a story: the full text from its page, its
// photo kept on our side, and the text rewritten in our words when the AI
// allows; otherwise a brief that links to the source.
func (w *worker) build(ctx context.Context, it rss.Item, source string) (Article, bool) {
	resolved, image, body := w.rss.Enrich(ctx, it.Link)
	if resolved == "" {
		resolved = it.Link
	}
	if image == "" {
		image = it.ImageURL
	}
	if image == "" || placeholderImage(image) {
		return Article{}, false // The Daily Star shows every story with a photo
	}
	if w.images != nil {
		if local, err := w.images.Fetch(ctx, image); err == nil {
			image = local
		} else {
			return Article{}, false
		}
	}
	title := strings.TrimSpace(plain(it.Title))
	if title == "" || advertorial(title) {
		return Article{}, false
	}
	text := plain(body)
	fed := strings.TrimSuffix(plain(it.Content), " Details") // Dhaka Tribune's "Details" link
	if len(text) < 400 && len(fed) > len(text) {
		text = fed // the page could not be read, but the feed carries the story
	}
	summary := fed
	if summary == "" || len(summary) > 400 {
		summary = firstSentences(text, 260)
	}
	published := it.Published
	if published.IsZero() || published.After(time.Now()) {
		published = time.Now()
	}
	section, sub := classify(resolved, title)
	a := Article{
		Title: title, Summary: summary, ImageURL: image, SourceName: source, SourceURL: it.Link,
		Section: section, Sub: sub, PublishedAt: published,
	}
	if len(text) > 400 && w.canRewrite() {
		if head, intro, paras, err := w.rewrite(ctx, title, text); err == nil {
			now := time.Now()
			a.Title, a.Content, a.Rewritten, a.RewrittenAt = head, paras, true, &now
			if intro != "" {
				a.Summary = intro
			}
		} else {
			log.Printf("[en] rewrite %q: %v", title, err)
		}
	}
	return a, true
}

func firstSentences(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if i := strings.LastIndex(cut, ". "); i > 80 {
		return cut[:i+1]
	}
	if i := strings.LastIndex(cut, " "); i > 0 {
		return cut[:i] + "…"
	}
	return cut
}

func (w *worker) canRewrite() bool {
	if w.ai == nil || w.rewrites <= 0 {
		return false
	}
	var n int64
	day := time.Now().Add(-24 * time.Hour)
	w.db.Model(&Article{}).Where("rewritten_at > ? OR (rewritten_at IS NULL AND rewritten AND created_at > ?)", day, day).Count(&n)
	return n < int64(w.rewrites)
}

// fillEdition gives the stories of today's e-paper their full text. A story
// that came in as a brief (the day's rewrites had run out) is read again at
// its source and rewritten in our words, front page first, so the printed
// pages carry whole stories, not a line or two.
func (w *worker) fillEdition(ctx context.Context, now time.Time) {
	// A story whose full text cannot be had is swapped for another of its
	// page's kind, which the next round fills.
	for round := 0; round < 4 && ctx.Err() == nil; round++ {
		if !w.fillRound(ctx, now) {
			return
		}
		n, err := replaceInEdition(w.db, now, w.failed)
		if err != nil {
			log.Printf("[en] e-paper: %v", err)
		}
		if n == 0 {
			return
		}
		log.Printf("[en] e-paper %s: %d stories without their full text swapped for others", now.Format("2006-01-02"), n)
	}
}

// fillRound rewrites the edition's briefs once; it reports whether any
// story failed (and so could be swapped).
func (w *worker) fillRound(ctx context.Context, now time.Time) bool {
	date := now.Format("2006-01-02")
	ed, pages, err := editionFor(w.db, date)
	if err != nil || ed.Date != date {
		return false
	}
	var ids []string
	for _, p := range pages {
		ids = append(ids, p.Stories...)
	}
	var briefs []Article
	w.db.Where("id IN ? AND (content = '' OR content IS NULL)", ids).Find(&briefs)
	byID := map[string]Article{}
	for _, a := range briefs {
		byID[a.ID.String()] = a
	}
	done, failed, aiErrors := 0, false, 0
	for _, id := range ids {
		a, ok := byID[id]
		if !ok || w.failed[id] {
			continue
		}
		if ctx.Err() != nil || !w.canRewrite() {
			break
		}
		_, _, body := w.rss.Enrich(ctx, a.SourceURL)
		text := plain(body)
		if len(text) < 400 {
			w.failed[id] = true // the outlet's page could not be read
			failed = true
			continue
		}
		head, intro, paras, err := w.rewrite(ctx, a.Title, text)
		if err != nil {
			log.Printf("[en] e-paper rewrite %q: %v", a.Title, err)
			if aiErrors++; aiErrors >= 3 {
				break // the AI is out of quota or down: try again next run
			}
			if w.tries[id]++; w.tries[id] >= 2 {
				w.failed[id] = true
				failed = true
			}
			continue
		}
		aiErrors = 0
		upd := map[string]any{"title": head, "content": paras, "rewritten": true, "rewritten_at": time.Now()}
		if intro != "" {
			upd["summary"] = intro
		}
		if w.db.Model(&Article{}).Where("id = ?", a.ID).Updates(upd).Error == nil {
			done++
		}
	}
	if done > 0 {
		log.Printf("[en] e-paper %s: %d stories given their full text", date, done)
	}
	return failed
}

// reclassify moves stories an older rule put in the wrong section: headlines
// taken for sports or business by a single word ("Awami League" for
// football, "protest" for a cricket Test).
func (w *worker) reclassify() {
	var list []Article
	w.db.Select("id", "title", "source_url", "section", "sub").
		Where("section IN ?", []string{"sports", "business"}).Find(&list)
	moved := 0
	for _, a := range list {
		if s, _ := placeByPath(a.SourceURL); s != "" {
			continue // placed by its address, which no rule change touches
		}
		section, sub := classify(a.SourceURL, a.Title)
		if section != a.Section {
			if w.db.Model(&Article{}).Where("id = ?", a.ID).
				Updates(map[string]any{"section": section, "sub": sub}).Error == nil {
				moved++
			}
		}
	}
	if moved > 0 {
		log.Printf("[en] %d stories moved to their right section", moved)
	}
}

const rewriteSystem = `You are a sub-editor at an English-language daily in Dhaka. Rewrite the news story you are given for our readers, in your own words.
Rules:
- Keep every fact, name, number, date, place and the attribution of every quote exactly as given. Add nothing that is not in the text.
- Do not copy sentences from the original; restructure and rephrase. Short quotes may be kept in quotation marks with their speaker.
- Neutral news style in the British English used by Bangladeshi newspapers. Lead with the most important fact.
- Never mention the outlet the story came from.
Return only JSON: {"headline": "...", "intro": "one sentence summing the story up", "paragraphs": ["...", "..."]}`

func (w *worker) rewrite(ctx context.Context, title, text string) (head, intro, paras string, err error) {
	if len(text) > 7000 {
		text = text[:7000]
	}
	res, _ := w.ai.Run(ctx, ai.GatewayRequest{
		Task: "rewrite", System: rewriteSystem, JSON: true, MaxTokens: 3000,
		User: "Headline: " + title + "\n\nStory:\n" + text,
	})
	if res.Err != nil {
		return "", "", "", res.Err
	}
	raw := strings.TrimSpace(res.Text)
	if i := strings.Index(raw, "{"); i >= 0 {
		raw = raw[i:]
	}
	if i := strings.LastIndex(raw, "}"); i >= 0 {
		raw = raw[:i+1]
	}
	var out struct {
		Headline   string   `json:"headline"`
		Intro      string   `json:"intro"`
		Paragraphs []string `json:"paragraphs"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return "", "", "", fmt.Errorf("bad JSON from %s: %w", res.Provider, err)
	}
	if strings.TrimSpace(out.Headline) == "" || len(out.Paragraphs) < 2 {
		return "", "", "", fmt.Errorf("incomplete rewrite from %s", res.Provider)
	}
	var b strings.Builder
	for _, p := range out.Paragraphs {
		if p = strings.TrimSpace(p); p != "" {
			b.WriteString("<p>" + html.EscapeString(p) + "</p>\n")
		}
	}
	return strings.TrimSpace(out.Headline), strings.TrimSpace(out.Intro), b.String(), nil
}

// --- schedule -----------------------------------------------------------------

var dhaka = time.FixedZone("Asia/Dhaka", 6*3600)

// loop reads the feeds every 20 minutes and makes the day's e-paper once
// midnight has passed in Dhaka.
func (w *worker) loop(ctx context.Context) {
	tick := time.NewTicker(20 * time.Minute)
	defer tick.Stop()
	select {
	case <-ctx.Done():
		return
	case <-time.After(45 * time.Second):
	}
	w.reclassify()
	for {
		// Today's e-paper first: made (or remade by newer rules) and its
		// stories given their full text, before new stories take the day's
		// rewrites. A copy that takes the hosted site's e-paper makes none.
		if !w.mirrored {
			if err := ensureEdition(w.db, time.Now().In(dhaka)); err != nil {
				log.Printf("[en] e-paper: %v", err)
			}
			fillCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			w.fillEdition(fillCtx, time.Now().In(dhaka))
			cancel()
		}
		runCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		w.run(runCtx)
		cancel()
		if !w.mirrored {
			if err := ensureEdition(w.db, time.Now().In(dhaka)); err != nil {
				log.Printf("[en] e-paper: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// placeholderImage reports an outlet's stand-in picture (its logo, shown
// when a story has no photo of its own).
func placeholderImage(u string) bool {
	l := strings.ToLower(u)
	return strings.Contains(l, "logo") || strings.Contains(l, "default-image") || strings.Contains(l, "placeholder")
}

// advertorial reports paid content an outlet's feed carries among its news
// ("Promotional Content: …"); the portal shows no advertising.
func advertorial(title string) bool {
	l := strings.ToLower(title)
	for _, p := range []string{"promotional content", "sponsored", "advertorial", "press release"} {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}
