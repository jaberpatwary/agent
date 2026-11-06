package agentic

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bangla-news-aggregator/internal/imagestore"
	"bangla-news-aggregator/internal/rss"
)

// GoogleNewsSearch searches Google News' public RSS search (no API key, no
// AI quota) — the agent's main evidence source for verification.
func GoogleNewsSearch(client *http.Client) SearchFunc {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return func(ctx context.Context, query, lang string) ([]NewsHit, error) {
		params := "hl=bn&gl=BD&ceid=BD:bn"
		if lang == "en" {
			params = "hl=en-US&gl=US&ceid=US:en"
		}
		u := "https://news.google.com/rss/search?q=" + url.QueryEscape(query) + "&" + params
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BanglaNewsDesk/1.0)")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("http %d", resp.StatusCode)
		}
		var feed struct {
			Items []struct {
				Title   string `xml:"title"`
				Link    string `xml:"link"`
				PubDate string `xml:"pubDate"`
				Source  string `xml:"source"`
			} `xml:"channel>item"`
		}
		if err := xml.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&feed); err != nil {
			return nil, err
		}
		var hits []NewsHit
		for _, it := range feed.Items {
			if len(hits) >= 8 {
				break
			}
			title := strings.TrimSpace(it.Title)
			if it.Source != "" {
				title = strings.TrimSpace(strings.TrimSuffix(title, " - "+it.Source))
			}
			t, _ := time.Parse(time.RFC1123, it.PubDate)
			hits = append(hits, NewsHit{Title: title, Source: strings.TrimSpace(it.Source), URL: it.Link, Published: t})
		}
		return hits, nil
	}
}

// PageReader extracts a page's main article text with the portal's scraper.
func PageReader(client *rss.Client) ReadFunc {
	return func(ctx context.Context, pageURL string) (string, error) {
		pageURL = strings.TrimSpace(pageURL)
		if err := imagestore.CheckURL(pageURL); err != nil {
			return "", fmt.Errorf("cannot read this URL: %v", err)
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		resolved, _, content := client.Enrich(ctx, pageURL)
		text := plainText(content)
		if len([]rune(text)) < 200 {
			return "", errors.New("could not extract readable article text from this page")
		}
		if resolved == "" {
			resolved = pageURL
		}
		return "URL: " + resolved + "\n\n" + clip(text, 5000), nil
	}
}
