package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	url := "https://samakal.com/bangladesh/article/379991/ডেঙ্গুতে-এক-দিনে-রেকর্ড-১১-জনের-মৃত্যু"
	client := &http.Client{}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, _ := client.Do(req)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(string(body)))

	// Let's find all divs with class containing detail or content
	doc.Find("div, p").Each(func(i int, s *goquery.Selection) {
		class, _ := s.Attr("class")
		if strings.Contains(class, "detail") || strings.Contains(class, "news") || strings.Contains(class, "content") || strings.Contains(class, "text") {
			if len(strings.TrimSpace(s.Text())) > 50 {
				fmt.Printf("Class [%s]: %s\n\n", class, strings.TrimSpace(s.Text())[:min(100, len(strings.TrimSpace(s.Text())))])
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
