package main

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mmcdole/gofeed"
)

func testFeedWithUA(name, url string) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("❌ %s request error: %v\n", name, err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("%s HTTP status: %d, Content-Type: %s, BodyLen: %d\n", name, resp.StatusCode, resp.Header.Get("Content-Type"), len(body))

	fp := gofeed.NewParser()
	feed, err := fp.ParseString(string(body))
	if err != nil {
		fmt.Printf("❌ %s parse error: %v (first 100 bytes: %s)\n", name, err, string(body[:min(100, len(body))]))
		return
	}
	fmt.Printf("✅ %s parsed successfully! Items: %d\n", name, len(feed.Items))
	if len(feed.Items) > 0 {
		it := feed.Items[0]
		fmt.Printf("   Item 1: %s | ContentLen: %d\n", it.Title, len(it.Content)+len(it.Description))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	testFeedWithUA("Samakal", "https://samakal.com/feed")
	testFeedWithUA("Jugantor", "https://www.jugantor.com/feed/rss.xml")
	testFeedWithUA("Kaler Kantho", "https://www.kalerkantho.com/feed/rss.xml")
	testFeedWithUA("Banglanews24", "https://www.banglanews24.com/feed/rss.xml")
	testFeedWithUA("Bdnews24", "https://bangla.bdnews24.com/feed")
	testFeedWithUA("Manab Zamin", "https://mzamin.com/rss.xml")
}
