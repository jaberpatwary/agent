package handler

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// The front page ends with "স্টার মাল্টিমিডিয়া", The Daily Star's own video
// section. The news pipeline has no videos, so the section shows The Daily
// Star's current ones: each links to its page on bangla.thedailystar.net.

const (
	multimediaSource = "https://bangla.thedailystar.net/"
	multimediaTTL    = 30 * time.Minute
)

// MultimediaItem is one card of the section.
type MultimediaItem struct {
	Title string
	URL   string // the video's page on bangla.thedailystar.net
	Image string
	Time  string // ISO time for the page's time element
	When  string // the date as The Daily Star prints it
	Label string // e.g. স্টার মাল্টিমিডিয়া, খেলা মাল্টিমিডিয়া
}

type multimediaCache struct {
	mu      sync.Mutex
	items   []MultimediaItem
	fetched time.Time
	fetch   func(ctx context.Context) ([]MultimediaItem, error)
}

var multimedia = &multimediaCache{fetch: fetchMultimedia}

// get returns the cached cards, refreshing them when they are older than the
// TTL. A failed refresh keeps the last good cards, so the section never turns
// into an error page; with nothing cached yet it is simply left out.
func (m *multimediaCache) get() []MultimediaItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.items) > 0 && time.Since(m.fetched) < multimediaTTL {
		return m.items
	}
	// Try again at most once a minute while the source is unreachable.
	if time.Since(m.fetched) < time.Minute {
		return m.items
	}
	m.fetched = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if items, err := m.fetch(ctx); err == nil && len(items) > 0 {
		m.items = items
	}
	return m.items
}

func fetchMultimedia(ctx context.Context) ([]MultimediaItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, multimediaSource, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; PortalBot/1.0)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, io.ErrUnexpectedEOF
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseMultimedia(doc), nil
}

// parseMultimedia reads the cards of the "স্টার মাল্টিমিডিয়া" block of the
// home page.
func parseMultimedia(doc *goquery.Document) []MultimediaItem {
	base, _ := url.Parse(multimediaSource)
	abs := func(ref string) string {
		u, err := base.Parse(strings.TrimSpace(ref))
		if err != nil {
			return ""
		}
		return u.String()
	}

	var items []MultimediaItem
	doc.Find(".news-section").EachWithBreak(func(_ int, section *goquery.Selection) bool {
		if href, _ := section.Find(".section-title a").First().Attr("href"); !strings.HasSuffix(strings.TrimRight(href, "/"), "/star-multimedia") {
			return true
		}
		section.Find(".card").Each(func(_ int, card *goquery.Selection) {
			link := card.Find(".card-title a").First()
			href, _ := link.Attr("href")
			title := strings.Join(strings.Fields(link.Text()), " ")
			img := card.Find("img").First()
			src, _ := img.Attr("src")
			if href == "" || title == "" || src == "" {
				return
			}
			when := card.Find("time").First()
			iso, _ := when.Attr("datetime")
			label := strings.Join(strings.Fields(card.Find(".card-info span.flex").First().Text()), " ")
			items = append(items, MultimediaItem{
				Title: title,
				URL:   abs(href),
				Image: abs(src),
				Time:  iso,
				When:  strings.Join(strings.Fields(when.Text()), " "),
				Label: label,
			})
		})
		return false
	})
	return items
}

// videoPart is part for the multimedia cards.
func videoPart(list []MultimediaItem, from, to int) []MultimediaItem {
	if to > len(list) {
		to = len(list)
	}
	if from >= to {
		return nil
	}
	return list[from:to]
}
