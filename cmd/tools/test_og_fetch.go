package main

import (
	"fmt"
	"net/http"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	urls := []string{
		"https://www.channelionline.com/body-of-one-person-who-went-missing-after-entering-the-water-near-the-padma-bridge-has-been-recovered/",
		"https://techshohor.com/201254/",
		"https://www.jagonews24.com/sports/cricket/1172557",
	}

	client := &http.Client{}
	for _, u := range urls {
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("err: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		doc, _ := goquery.NewDocumentFromReader(resp.Body)
		img, _ := doc.Find(`meta[property="og:image"]`).Attr("content")
		if img == "" {
			img, _ = doc.Find(`meta[name="twitter:image"]`).Attr("content")
		}
		fmt.Printf("URL: %s\n-> Image: %s\n\n", u, img)
	}
}
