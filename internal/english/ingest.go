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
	rewrites int // a day's cap on AI rewrites, shared with nothing else
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
	if title == "" {
		return Article{}, false
	}
	text := plain(body)
	summary := plain(it.Content)
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
			a.Title, a.Content, a.Rewritten = head, paras, true
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
	w.db.Model(&Article{}).Where("rewritten AND created_at > ?", time.Now().Add(-24*time.Hour)).Count(&n)
	return n < int64(w.rewrites)
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
	for {
		runCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		w.run(runCtx)
		cancel()
		if err := ensureEdition(w.db, time.Now().In(dhaka)); err != nil {
			log.Printf("[en] e-paper: %v", err)
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
