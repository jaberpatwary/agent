package rss

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
	gofeedrss "github.com/mmcdole/gofeed/rss"
)

var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

// Item is a normalized representation of a single RSS/Atom feed entry.
type Item struct {
	Title         string
	Content       string
	Categories    []string
	Link          string
	OriginalLink  string
	PublisherName string
	ImageURL      string
	Published     time.Time
}

// Client wraps the RSS parser and enriches incomplete feed entries from their source pages.
type Client struct {
	parser     *gofeed.Parser
	httpClient *http.Client
}

func NewClient() *Client {
	parser := gofeed.NewParser()
	parser.RSSTranslator = sourceAwareRSSTranslator{}
	return &Client{
		parser: parser,
		httpClient: &http.Client{
			Timeout: 12 * time.Second,
		},
	}
}

// gofeed's universal item omits RSS <source url="...">. Google News uses
// that element to provide the publisher URL for its wrapped article links.
type sourceAwareRSSTranslator struct{}

func (sourceAwareRSSTranslator) Translate(raw interface{}) (*gofeed.Feed, error) {
	feed, ok := raw.(*gofeedrss.Feed)
	if !ok {
		return (&gofeed.DefaultRSSTranslator{}).Translate(raw)
	}
	translated, err := (&gofeed.DefaultRSSTranslator{}).Translate(raw)
	if err != nil {
		return nil, err
	}
	for i, rawItem := range feed.Items {
		if i >= len(translated.Items) || rawItem.Source == nil || rawItem.Source.URL == "" {
			continue
		}
		if translated.Items[i].Custom == nil {
			translated.Items[i].Custom = make(map[string]string)
		}
		translated.Items[i].Custom["source_url"] = rawItem.Source.URL
		translated.Items[i].Custom["source_name"] = rawItem.Source.Title
	}
	return translated, nil
}

// Fetch uses complete RSS content as supplied, and scrapes the publisher only
// when the feed omits an image or appears to contain only a short excerpt.
func (c *Client) Fetch(ctx context.Context, feedURL string) ([]Item, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	feed, err := c.parser.ParseURLWithContext(feedURL, ctx)
	if err != nil {
		return nil, fmt.Errorf("rss: fetch %s: %w", feedURL, err)
	}

	items := make([]Item, 0, len(feed.Items))
	for _, entry := range feed.Items {
		item := Item{Title: entry.Title, Categories: append([]string(nil), entry.Categories...), Link: entry.Link, OriginalLink: entry.Link}
		if entry.Custom != nil {
			item.PublisherName = strings.TrimSpace(entry.Custom["source_name"])
		}
		// Google News RSS <source url> is the publisher homepage, not the
		// individual story URL. Keep the Google article URL so its redirect can
		// resolve to the correct story page during enrichment.
		if entry.Content != "" {
			item.Content = entry.Content
		} else {
			item.Content = entry.Description
		}
		item.ImageURL = resolveURL(item.Link, extractImage(entry))

		cleanLen := len(strings.TrimSpace(htmlTagRegex.ReplaceAllString(item.Content, "")))
		paraCount := strings.Count(strings.ToLower(item.Content), "<p")
		if item.Link != "" && (item.ImageURL == "" || cleanLen < 1500 || paraCount < 3 || isGoogleNewsURL(item.Link)) {
			resolvedLink, pageImage, fullText := c.scrapeArticle(ctx, item.Link)
			if isGoogleNewsURL(item.Link) && resolvedLink != "" && !isGoogleNewsURL(resolvedLink) {
				item.Link = resolvedLink
			}
			if pageImage != "" {
				item.ImageURL = pageImage
			}
			if len(fullText) > cleanLen && len(fullText) > 300 {
				item.Content = fullText
			}
		}

		if entry.PublishedParsed != nil {
			pub := *entry.PublishedParsed
			now := time.Now()
			// Daily Bangladesh currently labels Dhaka local RSS times as UTC.
			// Reinterpret only near-future dates from that publisher as UTC+6.
			if isDailyBangladeshURL(item.Link) && pub.After(now) && pub.Sub(now) <= 7*time.Hour {
				bangladeshTime := time.FixedZone("Bangladesh Standard Time", 6*60*60)
				pub = time.Date(pub.Year(), pub.Month(), pub.Day(), pub.Hour(), pub.Minute(), pub.Second(), pub.Nanosecond(), bangladeshTime)
			}
			maxAge := time.Now().Add(-7 * 24 * time.Hour)
			if pub.Before(maxAge) {
				pub = maxAge
			}
			if pub.After(time.Now()) {
				pub = time.Now()
			}
			item.Published = pub
		} else {
			item.Published = time.Now()
		}
		items = append(items, item)
	}
	return items, nil
}

func extractImage(entry *gofeed.Item) string {
	if entry.Image != nil && entry.Image.URL != "" {
		return entry.Image.URL
	}
	for _, enclosure := range entry.Enclosures {
		if enclosure.URL != "" && (strings.Contains(strings.ToLower(enclosure.Type), "image") || isImageURL(enclosure.URL)) {
			return enclosure.URL
		}
	}
	if media, ok := entry.Extensions["media"]; ok {
		for _, key := range []string{"thumbnail", "content"} {
			if values, ok := media[key]; ok && len(values) > 0 {
				if imageURL := values[0].Attrs["url"]; imageURL != "" {
					return imageURL
				}
			}
		}
	}
	for _, markup := range []string{entry.Content, entry.Description} {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(markup))
		if err != nil {
			continue
		}
		var imageURL string
		doc.Find("img").EachWithBreak(func(_ int, image *goquery.Selection) bool {
			for _, key := range []string{"src", "data-src", "data-original", "data-lazy-src", "srcset"} {
				if value, ok := image.Attr(key); ok {
					imageURL = firstSrc(value)
					if imageURL != "" {
						return false
					}
				}
			}
			return true
		})
		if imageURL != "" {
			return imageURL
		}
	}
	return ""
}

var articleBodySelectors = []string{
	"[itemprop='articleBody']", ".details-body", ".details-main", ".dNewsDesc",
	".entry-content", ".post-content", ".detail-box", ".details-text", ".content-details",
	".news-details", ".jw-detail", ".story-body", ".article-body", ".article-content",
	".news-content", ".content-body", ".post-body", ".story-content", ".story-element-text",
	".news-description", ".article-description", ".single-post-content", "article", "main",
}

// Enrich scrapes a publisher article and is shared with the historical backfill command.
func (c *Client) Enrich(ctx context.Context, pageURL string) (resolvedURL, imageURL, content string) {
	ctx, cancel := context.WithTimeout(ctx, c.httpClient.Timeout)
	defer cancel()
	return c.scrapeArticle(ctx, pageURL)
}

func (c *Client) scrapeArticle(ctx context.Context, pageURL string) (string, string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", "", ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "bn-BD,bn;q=0.9,en-US;q=0.8,en;q=0.7")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", ""
	}
	doc, err := goquery.NewDocumentFromReader(http.MaxBytesReader(nil, resp.Body, 6<<20))
	if err != nil {
		return "", "", ""
	}
	resolvedURL := pageURL
	if resp.Request != nil && resp.Request.URL != nil {
		resolvedURL = resp.Request.URL.String()
	}
	imageURL, _ := doc.Find(`meta[property="og:image"]`).Attr("content")
	if imageURL == "" {
		imageURL, _ = doc.Find(`meta[property="og:image:secure_url"]`).Attr("content")
	}
	if imageURL == "" {
		imageURL, _ = doc.Find(`meta[name="twitter:image"]`).Attr("content")
	}
	if imageURL == "" {
		imageURL, _ = doc.Find(`meta[itemprop="image"], meta[name="thumbnail"]`).First().Attr("content")
	}
	if imageURL == "" {
		image := doc.Find(".featured-image img, figure img, article img, .lead-image img, .detail-box img, [itemprop='image']").First()
		for _, key := range []string{"src", "data-src", "data-original", "data-lazy-src", "srcset"} {
			if value, ok := image.Attr(key); ok {
				imageURL = firstSrc(value)
				if imageURL != "" {
					break
				}
			}
		}
	}
	imageURL = resolveURL(resolvedURL, imageURL)
	return resolvedURL, imageURL, extractArticleText(doc)
}

// extractArticleText returns the story's full text as <p> paragraphs. It
// tries every known body container and keeps the most complete result:
//   - one container on its own (the usual layout),
//   - all containers of one kind joined in page order — some sites split a
//     story into many blocks (one per few paragraphs), and taking only the
//     biggest block gave half a story,
//   - the articleBody the site declares in its JSON-LD metadata, which is the
//     whole story on many news sites.
func extractArticleText(doc *goquery.Document) string {
	declared := jsonLDArticleBody(doc) // read before scripts are stripped
	doc.Find("script,style,noscript,nav,header,footer,aside,form,iframe,svg,button,[role='navigation'],.related,.recommended,.recommend,.share,.social,.advertisement,.ads,.comments,.comment,.article-tags,.tags").Remove()
	bestText, bestScore := "", 0
	consider := func(paragraphs []string) {
		candidate := strings.Join(paragraphs, "\n")
		score := len([]rune(candidate)) + len(paragraphs)*80
		if score > bestScore {
			bestScore, bestText = score, candidate
		}
	}
	for _, selector := range articleBodySelectors {
		matches := doc.Find(selector)
		matches.Each(func(_ int, container *goquery.Selection) {
			consider(containerParagraphs(container, map[string]bool{}))
		})
		// Generic page landmarks (article, main) can repeat for teaser cards;
		// only real body containers are joined.
		if matches.Length() > 1 && selector != "article" && selector != "main" {
			seen := map[string]bool{}
			var all []string
			matches.Each(func(_ int, container *goquery.Selection) {
				if container.ParentsFiltered(selector).Length() > 0 {
					return // nested inside another match, already covered by it
				}
				all = append(all, containerParagraphs(container, seen)...)
			})
			consider(all)
		}
	}
	// The declared body wins only when it is clearly more complete; it has no
	// markup, so the page's own paragraphs are preferred when they are as full.
	if bestRunes := utf8.RuneCountInString(htmlTagRegex.ReplaceAllString(bestText, "")); utf8.RuneCountInString(declared) > bestRunes*115/100 {
		var paragraphs []string
		for _, p := range strings.Split(declared, "\n") {
			if p = normalizeText(p); p != "" && !isBoilerplate(p) {
				paragraphs = append(paragraphs, "<p>"+html.EscapeString(p)+"</p>")
			}
		}
		return strings.Join(paragraphs, "\n")
	}
	return bestText
}

func containerParagraphs(container *goquery.Selection, seen map[string]bool) []string {
	paragraphs := make([]string, 0, 8)
	container.Find("p").Each(func(_ int, paragraph *goquery.Selection) {
		text := normalizeText(paragraph.Text())
		if len([]rune(text)) >= 25 && !isBoilerplate(text) && !seen[text] {
			seen[text] = true
			paragraphs = append(paragraphs, "<p>"+html.EscapeString(text)+"</p>")
		}
	})
	if len(paragraphs) == 0 {
		text := normalizeText(container.Text())
		if len([]rune(text)) >= 300 && !isBoilerplate(text) && !seen[text] {
			seen[text] = true
			paragraphs = append(paragraphs, "<p>"+html.EscapeString(text)+"</p>")
		}
	}
	return paragraphs
}

// jsonLDArticleBody returns the longest articleBody declared in the page's
// JSON-LD (schema.org NewsArticle and friends), with HTML stripped.
func jsonLDArticleBody(doc *goquery.Document) string {
	best := ""
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			if body, ok := x["articleBody"].(string); ok {
				body = html.UnescapeString(paraBreakRegex.ReplaceAllString(body, "\n"))
				body = strings.TrimSpace(htmlTagRegex.ReplaceAllString(body, " "))
				if utf8.RuneCountInString(body) > utf8.RuneCountInString(best) {
					best = body
				}
			}
			for _, e := range x {
				walk(e)
			}
		}
	}
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		var v any
		if json.Unmarshal([]byte(strings.TrimSpace(s.Text())), &v) == nil {
			walk(v)
		}
	})
	return best
}

var paraBreakRegex = regexp.MustCompile(`(?i)</p>|<br\s*/?>|\r?\n`)

func normalizeText(value string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(value), " "))
}

func isBoilerplate(text string) bool {
	lower := strings.ToLower(text)
	return strings.HasPrefix(text, "ছবির উৎস") || strings.HasPrefix(text, "ছবি ক্যাপশন") ||
		strings.Contains(lower, "all rights reserved") || strings.HasPrefix(text, "বিজ্ঞাপন :") ||
		strings.Contains(lower, "blazing-fast frontend")
}

func resolveURL(baseURL, rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	return resolved.String()
}

func firstSrc(value string) string {
	if strings.Contains(value, ",") {
		value = strings.TrimSpace(strings.Split(value, ",")[0])
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func isImageURL(rawURL string) bool {
	path := strings.ToLower(strings.SplitN(rawURL, "?", 2)[0])
	return strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".jpeg") || strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".webp") || strings.HasSuffix(path, ".gif")
}

func isGoogleNewsURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && strings.EqualFold(parsed.Hostname(), "news.google.com")
}

func isDailyBangladeshURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")
	return host == "daily-bangladesh.com" || strings.HasSuffix(host, ".daily-bangladesh.com")
}
