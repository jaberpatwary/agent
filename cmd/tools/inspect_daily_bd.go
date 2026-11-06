package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	url := "https://daily-bangladesh.com/international/539643"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(string(b)))

	fmt.Printf("HTML size: %d\n", len(b))
	// Check p tags
	doc.Find("p").Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if len([]rune(t)) > 20 {
			class, _ := s.Parent().Attr("class")
			id, _ := s.Parent().Attr("id")
			fmt.Printf("P tag [parent: %s id: %s]: %s\n\n", class, id, t[:min(80, len(t))])
		}
	})

	// Also check div tags with news text
	doc.Find("div").Each(func(i int, s *goquery.Selection) {
		class, _ := s.Attr("class")
		id, _ := s.Attr("id")
		if strings.Contains(class, "news") || strings.Contains(class, "detail") || strings.Contains(class, "content") || strings.Contains(id, "news") || strings.Contains(id, "content") {
			t := strings.TrimSpace(s.Text())
			if len([]rune(t)) > 100 {
				fmt.Printf("DIV tag [class: %s id: %s] Len: %d -> %s\n\n", class, id, len(t), t[:min(100, len(t))])
			}
		}
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
