package main

import (
	"fmt"
	"net/http"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

func main() {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL("https://news.google.com/rss/search?q=site:samakal.com&hl=bn&gl=BD&ceid=BD:bn")
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}

	client := &http.Client{}
	count := 0
	for _, it := range feed.Items {
		if count >= 5 {
			break
		}
		count++

		req, _ := http.NewRequest("GET", it.Link, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Item %d err: %v\n", count, err)
			continue
		}
		defer resp.Body.Close()

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			continue
		}

		ogImg, _ := doc.Find(`meta[property="og:image"]`).Attr("content")
		ogDesc, _ := doc.Find(`meta[property="og:description"]`).Attr("content")
		fmt.Printf("Item %d: %s\n-> og:image: %s\n-> og:desc: %s\n\n", count, it.Title, ogImg, ogDesc)
	}
}
