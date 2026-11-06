package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

func main() {
	cfg := config.Load()
	db, err := repository.NewPostgresDB(cfg)
	if err != nil {
		panic(err)
	}

	var articles []domain.Article
	db.Where("source_name = ?", "জাগো নিউজ ২৪").Limit(3).Find(&articles)

	client := &http.Client{Timeout: 12 * time.Second}

	for _, a := range articles {
		fmt.Printf("\n=== Article URL: %s ===\n", a.SourceURL)
		req, _ := http.NewRequest("GET", a.SourceURL, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
		req.Header.Set("Accept-Language", "bn-BD,bn;q=0.9")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		fmt.Printf("HTTP Status: %d\n", resp.StatusCode)

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
		if err != nil {
			fmt.Printf("Parse error: %v\n", err)
			continue
		}

		ogTitle := doc.Find(`meta[property="og:title"]`).AttrOr("content", "")
		ogImg := doc.Find(`meta[property="og:image"]`).AttrOr("content", "")
		h1 := strings.TrimSpace(doc.Find("h1").First().Text())
		fmt.Printf("OG Title: %s\n", ogTitle)
		fmt.Printf("OG Image: %s\n", ogImg)
		fmt.Printf("H1: %s\n", h1)

		// Print all paragraph texts from various selectors
		selectors := []string{
			".news-details-content p",
			".detail-content p",
			".jw-detail p",
			".news-body p",
			"[itemprop='articleBody'] p",
			"article p",
			".content-area p",
			".entry-content p",
			".post-content p",
		}
		for _, sel := range selectors {
			count := doc.Find(sel).Length()
			if count > 0 {
				fmt.Printf("  Selector '%s': %d paragraphs\n", sel, count)
				doc.Find(sel).Each(func(i int, s *goquery.Selection) {
					t := strings.TrimSpace(s.Text())
					if len(t) > 10 && i < 3 {
						fmt.Printf("    [%d]: %s\n", i, t[:min(80, len(t))])
					}
				})
				break
			}
		}

		// Also check body classes and main divs
		fmt.Printf("Body classes: %s\n", doc.Find("body").AttrOr("class", ""))

		// Print raw HTML snippet from first div with content
		rawSnippet := string(body)
		if idx := strings.Index(rawSnippet, "article-body"); idx >= 0 {
			end := idx + 200
			if end > len(rawSnippet) { end = len(rawSnippet) }
			fmt.Printf("article-body snippet: %s\n", rawSnippet[idx:end])
		}
	}
}

func min(a, b int) int {
	if a < b { return a }
	return b
}
