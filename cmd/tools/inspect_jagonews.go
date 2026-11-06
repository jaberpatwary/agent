package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	url := "https://www.jagonews24.com/sports/cricket/1172557"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(string(body)))

	fmt.Printf("Status: %d\n", resp.StatusCode)
	doc.Find("meta").Each(func(i int, s *goquery.Selection) {
		prop, _ := s.Attr("property")
		name, _ := s.Attr("name")
		content, _ := s.Attr("content")
		if strings.Contains(prop, "image") || strings.Contains(name, "image") || strings.Contains(prop, "og:") {
			fmt.Printf("Meta [%s / %s] = %s\n", prop, name, content)
		}
	})

	// also check article images
	doc.Find("figure img, .featured-image img, article img").Each(func(i int, s *goquery.Selection) {
		src, _ := s.Attr("src")
		fmt.Printf("Article img src: %s\n", src)
	})
}
