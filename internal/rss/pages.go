package rss

import (
	"context"
	"encoding/xml"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
)

// articlePage is what a publisher's article page yields.
type articlePage struct {
	resolved, image, title, text string
	read                         time.Time
}

// Feeds are read every few minutes and list the same stories each time, so a
// page that was read successfully is remembered for a while.
const pageCacheTTL = 3 * time.Hour

// siteBodies are the article-body boxes of outlets whose pages also carry
// other stories (related headlines, print-only blocks), which the generic
// "most complete box" search would pull into the story.
var siteBodies = map[string]string{
	"samakal.com":         "#contentDetails",
	"dailyjanakantha.com": "#contentDetails",
	"dailyinqilab.com":    ".description",
	"jugantor.com":        ".detailBody",
	"mzamin.com":          "#articleContent",
	"ittefaq.com.bd":      "[itemprop='articleBody']",
}

func hostOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(p.Hostname()), "www.")
}

func (c *Client) readPage(ctx context.Context, pageURL string) articlePage {
	c.mu.Lock()
	if p, ok := c.pages[pageURL]; ok && time.Since(p.read) < pageCacheTTL {
		c.mu.Unlock()
		return p
	}
	c.mu.Unlock()
	p := c.fetchPage(ctx, pageURL)
	if p.text != "" {
		c.mu.Lock()
		if len(c.pages) > 5000 {
			for k, old := range c.pages {
				if time.Since(old.read) >= pageCacheTTL {
					delete(c.pages, k)
				}
			}
		}
		c.pages[pageURL] = p
		c.mu.Unlock()
	}
	return p
}

func (c *Client) fetchPage(ctx context.Context, pageURL string) articlePage {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return articlePage{}
	}
	setBrowserHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return articlePage{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return articlePage{}
	}
	doc, err := goquery.NewDocumentFromReader(http.MaxBytesReader(nil, resp.Body, 6<<20))
	if err != nil {
		return articlePage{}
	}
	p := articlePage{resolved: pageURL, read: time.Now()}
	if resp.Request != nil && resp.Request.URL != nil {
		p.resolved = resp.Request.URL.String()
	}
	p.image = resolveURL(p.resolved, pageImage(doc))
	p.title = pageTitle(doc)
	p.text = extractArticleText(doc, p.resolved)
	return p
}

func setBrowserHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "bn-BD,bn;q=0.9,en-US;q=0.8,en;q=0.7")
}

func pageImage(doc *goquery.Document) string {
	for _, sel := range []string{`meta[property="og:image"]`, `meta[property="og:image:secure_url"]`, `meta[name="twitter:image"]`, `meta[itemprop="image"], meta[name="thumbnail"]`} {
		if v, _ := doc.Find(sel).First().Attr("content"); v != "" {
			return v
		}
	}
	image := doc.Find(".featured-image img, figure img, article img, .lead-image img, .detail-box img, [itemprop='image']").First()
	for _, key := range []string{"src", "data-src", "data-original", "data-lazy-src", "srcset"} {
		if value, ok := image.Attr(key); ok {
			if src := firstSrc(value); src != "" {
				return src
			}
		}
	}
	return ""
}

// pageTitle is the story's headline, without the " | Outlet" sites append.
func pageTitle(doc *goquery.Document) string {
	t, _ := doc.Find(`meta[property="og:title"]`).First().Attr("content")
	if t == "" {
		t = doc.Find("h1").First().Text()
	}
	if t == "" {
		t = doc.Find("title").First().Text()
	}
	t = normalizeText(t)
	for _, sep := range []string{" | ", " - ", " – "} {
		if i := strings.LastIndex(t, sep); i > 0 && utf8.RuneCountInString(t[i:]) <= 30 {
			t = strings.TrimSpace(t[:i])
		}
	}
	// "…: Daily Naya Diganta (দৈনিক নয়া দিগন্ত)"
	if i := strings.LastIndex(t, ": "); i > 0 && (strings.Contains(t[i:], "Daily") || strings.Contains(t[i:], "দৈনিক")) {
		t = strings.TrimSpace(t[:i])
	}
	return t
}

// siteBody reads a known outlet's body box, if this page has one.
func siteBody(doc *goquery.Document, pageURL string) string {
	sel, ok := siteBodies[hostOf(pageURL)]
	if !ok {
		return ""
	}
	box := doc.Find(sel).First()
	if box.Length() == 0 {
		return ""
	}
	paras := containerParagraphs(box, map[string]bool{})
	text := strings.Join(paras, "\n")
	if utf8.RuneCountInString(htmlTagRegex.ReplaceAllString(text, "")) < 200 {
		return ""
	}
	return text
}

var brSplit = regexp.MustCompile(`(?i)<br\s*/?>`)

// lines splits a block's HTML at its line breaks: some outlets put a whole
// story in one <p> with <br> between paragraphs.
func lines(blockHTML string) []string {
	var out []string
	for _, part := range brSplit.Split(blockHTML, -1) {
		if t := normalizeText(html.UnescapeString(htmlTagRegex.ReplaceAllString(part, " "))); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Sitemaps: outlets without an RSS feed list their stories in a news
// sitemap. "{date}" in a feed URL stands for today and yesterday (Dhaka),
// for outlets that publish one sitemap per day.

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
	News    struct {
		Title string `xml:"title"`
		Date  string `xml:"publication_date"`
	} `xml:"news"`
	Image struct {
		Loc string `xml:"loc"`
	} `xml:"image"`
}

const sitemapMaxStories = 30

// isSitemapFeed tells sitemap feeds from RSS ones by their address.
func isSitemapFeed(feedURL string) bool {
	return strings.Contains(strings.ToLower(feedURL), "sitemap")
}

var articleID = regexp.MustCompile(`/\d{4,}(/|$)`)

func (c *Client) fetchSitemap(ctx context.Context, feedURL string) ([]Item, error) {
	addrs := []string{feedURL}
	if strings.Contains(feedURL, "{date}") {
		now := time.Now().In(dhaka)
		addrs = []string{strings.ReplaceAll(feedURL, "{date}", now.Format("2006-01-02")),
			strings.ReplaceAll(feedURL, "{date}", now.AddDate(0, 0, -1).Format("2006-01-02"))}
	}
	var entries []sitemapURL
	var firstErr error
	for _, a := range addrs {
		got, err := c.readSitemap(ctx, a)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		entries = append(entries, got...)
	}
	if len(entries) == 0 {
		return nil, firstErr
	}
	stories := pickStories(entries)

	items := make([]Item, len(stories))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, e := range stories {
		wg.Add(1)
		go func(i int, e sitemapURL) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			items[i] = c.sitemapItem(ctx, e)
		}(i, e)
	}
	wg.Wait()
	out := items[:0]
	for _, it := range items {
		if it.Link != "" {
			out = append(out, it)
		}
	}
	return out, nil
}

func (c *Client) readSitemap(ctx context.Context, addr string) ([]sitemapURL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, err
	}
	setBrowserHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{addr, resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var set struct {
		URLs []sitemapURL `xml:"url"`
	}
	if err := xml.Unmarshal(body, &set); err != nil {
		return nil, err
	}
	return set.URLs, nil
}

type httpStatusError struct {
	url  string
	code int
}

func (e *httpStatusError) Error() string { return e.url + ": HTTP " + strconv.Itoa(e.code) }

// pickStories keeps a sitemap's article pages (an id in the path; section
// and archive pages are dropped), newest first.
func pickStories(entries []sitemapURL) []sitemapURL {
	seen := map[string]bool{}
	var out []sitemapURL
	for _, e := range entries {
		e.Loc = strings.TrimSpace(e.Loc)
		if e.Loc == "" || seen[e.Loc] || !articleID.MatchString(e.Loc) {
			continue
		}
		seen[e.Loc] = true
		out = append(out, e)
	}
	// Dated entries newest first; undated ones (bare URL lists) by their id.
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := sitemapTime(out[i]), sitemapTime(out[j])
		if !ti.IsZero() || !tj.IsZero() {
			return ti.After(tj)
		}
		return storyID(out[i].Loc) > storyID(out[j].Loc)
	})
	if len(out) > sitemapMaxStories {
		out = out[:sitemapMaxStories]
	}
	return out
}

func sitemapTime(e sitemapURL) time.Time {
	for _, v := range []string{e.News.Date, e.LastMod} {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(v)); err == nil {
			return t
		}
	}
	return time.Time{}
}

func storyID(u string) int {
	m := regexp.MustCompile(`/(\d{4,})(/|$)`).FindStringSubmatch(u)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// sitemapItem reads a listed story's page: sitemaps carry at most a title,
// never the text.
func (c *Client) sitemapItem(ctx context.Context, e sitemapURL) Item {
	p := c.readPage(ctx, e.Loc)
	if utf8.RuneCountInString(htmlTagRegex.ReplaceAllString(p.text, "")) < 300 {
		return Item{}
	}
	it := Item{Link: e.Loc, OriginalLink: e.Loc, Title: normalizeText(e.News.Title), Content: cleanContent(p.text), ImageURL: e.Image.Loc}
	if it.Title == "" {
		it.Title = p.title
	}
	if it.ImageURL == "" {
		it.ImageURL = p.image
	}
	if it.Title == "" || it.ImageURL == "" {
		return Item{}
	}
	it.Published = clampPublished(sitemapTime(e))
	if seg := strings.Split(strings.Trim(mustPath(e.Loc), "/"), "/"); len(seg) > 1 {
		it.Categories = []string{seg[0]} // the outlet's section, e.g. "sports"
	}
	return it
}

func mustPath(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return p.Path
}

var dhaka = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Dhaka"); err == nil {
		return loc
	}
	return time.FixedZone("BST", 6*3600)
}()

// clampPublished keeps a story's time within the last week and not in the
// future; unknown means now. Some outlets label Dhaka time as UTC, which
// puts a fresh story up to six hours ahead: that is read as Dhaka time.
func clampPublished(t time.Time) time.Time {
	now := time.Now()
	if t.After(now) && t.Sub(now) <= 6*time.Hour+10*time.Minute {
		t = t.Add(-6 * time.Hour)
	}
	switch {
	case t.IsZero(), t.After(now):
		return now
	case t.Before(now.Add(-7 * 24 * time.Hour)):
		return now.Add(-7 * 24 * time.Hour)
	}
	return t
}

var copyrightMark = regexp.MustCompile(`©\s*[^<\n]{0,40}`)

// cleanContent repairs feed text: some feeds send their HTML escaped twice
// ("&lt;p&gt;"), and some end with "© Outlet".
func cleanContent(s string) string {
	if !strings.Contains(s, "<") && strings.Contains(s, "&lt;") {
		s = html.UnescapeString(s)
	}
	return copyrightMark.ReplaceAllString(s, "")
}
