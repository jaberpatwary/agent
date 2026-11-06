package main

import (
	"fmt"
	"net/http"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

func main() {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL("https://www.risingbd.com/rss")
	if err == nil && len(feed.Items) > 0 {
		item := feed.Items[0]
		fmt.Printf("RisingBD Title: %s\nLink: %s\nEnclosures: %v\nImage: %v\nDesc: %s\n", item.Title, item.Link, item.Enclosures, item.Image, item.Description)

		// test og fetch
		req, _ := http.NewRequest("GET", item.Link, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			doc, _ := goquery.NewDocumentFromReader(resp.Body)
			ogImg, _ := doc.Find(`meta[property="og:image"]`).Attr("content")
			fmt.Printf("RisingBD og:image: %s\n", ogImg)
		}
	}

	feed2, err := fp.ParseURL("https://www.daily-bangladesh.com/feed")
	if err == nil && len(feed2.Items) > 0 {
		item := feed2.Items[0]
		fmt.Printf("\nDaily Bangladesh Title: %s\nLink: %s\nEnclosures: %v\nImage: %v\nDesc: %s\n", item.Title, item.Link, item.Enclosures, item.Image, item.Description)
		req, _ := http.NewRequest("GET", item.Link, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			doc, _ := goquery.NewDocumentFromReader(resp.Body)
			ogImg, _ := doc.Find(`meta[property="og:image"]`).Attr("content")
			fmt.Printf("Daily Bangladesh og:image: %s\n", ogImg)
		}
	}
}
