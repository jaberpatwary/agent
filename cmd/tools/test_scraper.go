package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

func testFeed(name, url string) {
	fp := gofeed.NewParser()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	feed, err := fp.ParseURLWithContext(url, ctx)
	if err != nil {
		fmt.Printf("Feed %s failed: %v\n", name, err)
		return
	}
	fmt.Printf("\n=== Feed: %s (%d items) ===\n", name, len(feed.Items))
	if len(feed.Items) > 0 {
		item := feed.Items[0]
		fmt.Printf("Title: %s\n", item.Title)
		fmt.Printf("Link: %s\n", item.Link)
		fmt.Printf("Desc len: %d\n", len(item.Description))
		fmt.Printf("Content len: %d\n", len(item.Content))
		hasImg := item.Image != nil || len(item.Enclosures) > 0
		fmt.Printf("Direct Image in RSS: %v\n", hasImg)

		// Check media extension
		if media, ok := item.Extensions["media"]; ok {
			fmt.Printf("Has media extensions: %v\n", media)
		}
	}
}

func testScrape(pageURL string) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", pageURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Scrape failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		fmt.Printf("Goquery failed: %v\n", err)
		return
	}

	ogImage, _ := doc.Find(`meta[property="og:image"]`).Attr("content")
	if ogImage == "" {
		ogImage, _ = doc.Find(`meta[name="twitter:image"]`).Attr("content")
	}

	var paragraphs []string
	doc.Find("article p, .story-element p, .content p, .article-content p, .dtl_content p, main p").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if len([]rune(text)) > 20 {
			paragraphs = append(paragraphs, text)
		}
	})

	fmt.Printf("\nScrape test for %s:\n", pageURL)
	fmt.Printf("Found og:image: %s\n", ogImage)
	fmt.Printf("Found %d paragraphs, total words: %d\n", len(paragraphs), len(strings.Fields(strings.Join(paragraphs, " "))))
	if len(paragraphs) > 0 {
		fmt.Printf("First para: %s\n", paragraphs[0])
	}
}

func main() {
	testFeed("Prothom Alo", "https://www.prothomalo.com/feed")
	testFeed("Jugantor Direct", "https://www.jugantor.com/feed/rss.xml")
	testFeed("Ittefaq Direct", "https://www.ittefaq.com.bd/feed")
	testFeed("Samakal Direct", "https://samakal.com/feed")
	testFeed("Bdnews24 Direct", "https://bangla.bdnews24.com/feed")
	testFeed("Jagonews24 Direct", "https://www.jagonews24.com/rss/rss.xml")
	testFeed("BBC Bangla", "https://feeds.bbci.co.uk/bengali/rss.xml")

	testScrape("https://www.jugantor.com/todays-paper/city/856860")
	testScrape("https://samakal.com/bangladesh/article/260759")
}
