package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

func main() {
	// 1. Check Jagonews24 item description
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL("https://www.jagonews24.com/rss/rss.xml")
	if err == nil && len(feed.Items) > 0 {
		item := feed.Items[0]
		fmt.Printf("Jagonews24 item Desc: %s\n", item.Description)
	}

	// 2. Check Google News redirect resolution
	// Let's test a Google news item link
	gfeed, err := fp.ParseURL("https://news.google.com/rss/search?q=site:samakal.com&hl=bn&gl=BD&ceid=BD:bn")
	if err == nil && len(gfeed.Items) > 0 {
		item := gfeed.Items[0]
		fmt.Printf("\nGoogle News Link: %s\n", item.Link)
		fmt.Printf("Google News Desc: %s\n", item.Description)

		client := &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				fmt.Printf("Redirecting to: %s\n", req.URL.String())
				return nil
			},
		}

		req, _ := http.NewRequest("GET", item.Link, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			finalURL := resp.Request.URL.String()
			fmt.Printf("Final URL: %s\n", finalURL)

			body, _ := io.ReadAll(resp.Body)
			html := string(body)

			// Check if Google returned a JS redirect page with the real URL
			// Usually: <c-wiz ... data-n-a-u="https://samakal.com/..." or href="https://samakal.com/..."
			re := regexp.MustCompile(`https?://[a-zA-Z0-9.-]+\.[a-z]{2,}/[^\s"'>]+`)
			urls := re.FindAllString(html, -1)
			for _, u := range urls {
				if strings.Contains(u, "samakal.com") {
					fmt.Printf("Found real newspaper URL inside Google page: %s\n", u)
					break
				}
			}

			doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
			if err == nil {
				ogImg, _ := doc.Find(`meta[property="og:image"]`).Attr("content")
				fmt.Printf("og:image from page: %s\n", ogImg)
			}
		}
	}
}
