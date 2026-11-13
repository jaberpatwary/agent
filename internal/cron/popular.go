package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
	"bangla-news-aggregator/internal/rss"
)

// Source outlets publish their own "most read" lists. The desk reads them on
// every run, covers those stories first, and the portal's "most read" box
// shows the ones it has published. Only rank is published, never counts.

// popularList reads one outlet's most-read list, best first.
type popularList func(ctx context.Context, c *http.Client) ([]popularHit, error)

type popularHit struct {
	URL, Title string
	Published  time.Time // zero if the list doesn't say
}

// popularSources maps a feed's host to the reader of its outlet's list.
var popularSources = map[string]popularList{
	"prothomalo.com":   prothomAloMostRead,
	"bbc.co.uk":        bbcBanglaMostRead,
	"bbc.com":          bbcBanglaMostRead,
	"feeds.bbci.co.uk": bbcBanglaMostRead,
}

// SetPopularRepo turns on the most-read lists.
func (w *Worker) SetPopularRepo(repo repository.PopularRepository) { w.popularRepo = repo }

func feedHost(feedURL string) string {
	u, err := url.Parse(feedURL)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// refreshPopular reads the most-read list of every active feed's outlet that
// publishes one, stores it, and returns it by URL.
func (w *Worker) refreshPopular(ctx context.Context, feeds []domain.FeedSource) map[string]domain.PopularStory {
	out := map[string]domain.PopularStory{}
	if w.popularRepo == nil {
		return out
	}
	client := &http.Client{Timeout: 20 * time.Second}
	done := map[string]bool{}
	for _, f := range feeds {
		read, ok := popularSources[feedHost(f.URL)]
		if !ok || done[f.Name] {
			continue
		}
		done[f.Name] = true
		hits, err := read(ctx, client)
		if err != nil || len(hits) == 0 {
			log.Printf("[popular] %s: most-read list unavailable: %v", f.Name, err)
			continue
		}
		now := time.Now()
		list := make([]domain.PopularStory, 0, len(hits))
		seen := map[string]bool{}
		for _, h := range hits {
			key := domain.PopularKey(h.URL)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			p := domain.PopularStory{URL: key, Outlet: f.Name, Title: strings.TrimSpace(h.Title), Rank: len(list) + 1, FetchedAt: now}
			list = append(list, p)
			out[key] = p
		}
		if err := w.popularRepo.Replace(f.Name, list); err != nil {
			log.Printf("[popular] %s: could not store the list: %v", f.Name, err)
		}
	}
	return out
}

// popularCandidates turns most-read stories that are not among the feeds'
// own items into candidates, read from their pages. Stories already handled
// are dropped before any page is fetched.
func (w *Worker) popularCandidates(ctx context.Context, feeds []domain.FeedSource, popular map[string]domain.PopularStory, have []candidate) []candidate {
	if len(popular) == 0 {
		return nil
	}
	inFeeds := map[string]bool{}
	for _, c := range have {
		inFeeds[domain.PopularKey(c.item.Link)] = true
	}
	byName := map[string]domain.FeedSource{}
	for _, f := range feeds {
		byName[f.Name] = f
	}
	var extra []candidate
	for key, p := range popular {
		if !inFeeds[key] {
			extra = append(extra, candidate{item: rss.Item{Link: key, Title: p.Title}, feed: byName[p.Outlet]})
		}
	}
	extra, _, err := w.dropAlreadySeen(extra)
	if err != nil {
		return nil
	}
	keys := make([]string, len(extra))
	for i, c := range extra {
		keys[i] = c.item.Link
	}
	handled, err := w.popularRepo.Handled(keys)
	if err != nil {
		return nil
	}
	kept := extra[:0]
	for _, c := range extra {
		if !handled[c.item.Link] {
			kept = append(kept, c)
		}
	}
	extra = kept
	sort.Slice(extra, func(i, j int) bool { return popular[extra[i].item.Link].Rank < popular[extra[j].item.Link].Rank })
	out := make([]candidate, 0, len(extra))
	for _, c := range extra {
		_, image, page := w.rssClient.Enrich(ctx, c.item.Link)
		if visibleLen(page) < 200 {
			continue
		}
		c.item.Content, c.item.ImageURL, c.item.Published = page, image, time.Now()
		if p, err := url.Parse(c.item.Link); err == nil {
			if seg := strings.Split(strings.Trim(p.Path, "/"), "/"); len(seg) > 1 {
				c.item.Categories = []string{seg[0]} // the outlet's section, e.g. "sports"
			}
		}
		out = append(out, c)
	}
	return out
}

// prothomAloMostRead reads Prothom Alo's "পঠিত" collection.
func prothomAloMostRead(ctx context.Context, c *http.Client) ([]popularHit, error) {
	body, err := httpGet(ctx, c, "https://www.prothomalo.com/api/v1/collections/mostread?limit=10&item-type=story&fields=headline,url,first-published-at")
	if err != nil {
		return nil, err
	}
	return parseProthomAloMostRead(body)
}

func parseProthomAloMostRead(body []byte) ([]popularHit, error) {
	var d struct {
		Items []struct {
			Story struct {
				Headline  string `json:"headline"`
				URL       string `json:"url"`
				Published int64  `json:"first-published-at"`
			} `json:"story"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	var out []popularHit
	for _, it := range d.Items {
		if it.Story.URL == "" {
			continue
		}
		h := popularHit{URL: it.Story.URL, Title: it.Story.Headline}
		if it.Story.Published > 0 {
			h.Published = time.UnixMilli(it.Story.Published)
		}
		out = append(out, h)
	}
	return out, nil
}

// bbcBanglaMostRead reads BBC Bangla's "সর্বাধিক পঠিত" page.
func bbcBanglaMostRead(ctx context.Context, c *http.Client) ([]popularHit, error) {
	body, err := httpGet(ctx, c, "https://www.bbc.com/bengali/popular/read")
	if err != nil {
		return nil, err
	}
	return parseBBCMostRead(body)
}

// parseBBCMostRead keeps the page's links that BBC itself marks as
// most-read promos, in page order.
func parseBBCMostRead(body []byte) ([]popularHit, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	var out []popularHit
	doc.Find(`a[href*="/bengali/articles/"]`).Each(func(_ int, a *goquery.Selection) {
		track, _ := a.Attr("data-static-reverb-click")
		if !strings.Contains(track, "most-read") {
			return
		}
		href, _ := a.Attr("href")
		if strings.HasPrefix(href, "/") {
			href = "https://www.bbc.com" + href
		}
		out = append(out, popularHit{URL: href, Title: strings.TrimSpace(a.Text())})
	})
	return out, nil
}

func httpGet(ctx context.Context, c *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "bn-BD,bn;q=0.9,en;q=0.8")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}
