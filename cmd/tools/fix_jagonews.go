package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"regexp"

	"github.com/PuerkitoBio/goquery"

	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

var htmlTag = regexp.MustCompile(`<[^>]*>`)

func getPage(client *http.Client, url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "bn-BD,bn;q=0.9")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 3*1024*1024))
}

func scrapeJagonews(client *http.Client, pageURL string) (title, content, imgURL string) {
	data, err := getPage(client, pageURL)
	if err != nil {
		return
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		return
	}

	title = strings.TrimSpace(doc.Find(`meta[property="og:title"]`).AttrOr("content", ""))
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1.title, h1.news-title, h1").First().Text())
	}

	imgURL = doc.Find(`meta[property="og:image"]`).AttrOr("content", "")
	if imgURL == "" {
		imgURL = doc.Find(`meta[name="twitter:image"]`).AttrOr("content", "")
	}
	if imgURL == "" {
		imgURL, _ = doc.Find(".news-details-img img, .lead-img img, .detail-box img, article img, .content img").First().Attr("src")
	}
	if imgURL != "" && strings.HasPrefix(imgURL, "/") && !strings.HasPrefix(imgURL, "//") {
		imgURL = "https://www.jagonews24.com" + imgURL
	}

	seenParas := make(map[string]bool)
	var paras []string
	selectors := ".news-details-content p, .detail-content p, .jw-detail p, .news-body p, [itemprop='articleBody'] p, article p, main p"
	doc.Find(selectors).Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if utf8.RuneCountInString(t) > 25 && !seenParas[t] {
			if strings.Contains(t, "বিজ্ঞাপন") || strings.Contains(t, "ALL RIGHTS RESERVED") {
				return
			}
			seenParas[t] = true
			paras = append(paras, "<p>"+t+"</p>")
		}
	})
	content = strings.Join(paras, "\n")
	return
}

func main() {
	cfg := config.Load()
	db, err := repository.NewPostgresDB(cfg)
	if err != nil {
		log.Fatalf("DB error: %v", err)
	}
	aiClient := ai.NewClient(cfg)

	httpClient := &http.Client{Timeout: 15 * time.Second}

	// Get all Jagonews articles without image or content from DB
	var articles []domain.Article
	db.Where("source_name = ? AND (image_url = '' OR image_url IS NULL OR length(content) < 500)", "জাগো নিউজ ২৪").Limit(50).Find(&articles)
	fmt.Printf("Found %d জাগো নিউজ ২৪ articles to enhance\n", len(articles))

	var mu sync.Mutex
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	updated := 0

	for _, a := range articles {
		wg.Add(1)
		sem <- struct{}{}
		go func(art domain.Article) {
			defer wg.Done()
			defer func() { <-sem }()

			title, content, imgURL := scrapeJagonews(httpClient, art.SourceURL)
			save := false

			if title != "" && art.Title == "" {
				art.Title = title
				save = true
			}
			if imgURL != "" && art.ImageURL == "" {
				art.ImageURL = imgURL
				save = true
			}
			if len(content) > len(art.Content) && len(content) > 300 {
				art.Content = content
				// Update summary too
				result, _ := aiClient.Summarize(context.Background(), art.Title, content)
				if result.Err == nil && result.Text != "" {
					art.Summary = result.Text
				} else {
					cleaned := htmlTag.ReplaceAllString(content, " ")
					cleaned = strings.Join(strings.Fields(cleaned), " ")
					if utf8.RuneCountInString(cleaned) > 280 {
						runes := []rune(cleaned)
						cleaned = string(runes[:280]) + "..."
					}
					art.Summary = cleaned
				}
				save = true
			}

			if save {
				db.Save(&art)
				mu.Lock()
				updated++
				fmt.Printf("✅ Enhanced জাগো নিউজ: [img=%v, contentLen=%d] %s\n", art.ImageURL != "", len(art.Content), art.Title)
				mu.Unlock()
			}
		}(a)
	}
	wg.Wait()
	fmt.Printf("\n✅ Enhanced %d জাগো নিউজ ২৪ articles\n", updated)
}
