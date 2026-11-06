package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	client := &http.Client{}
	req, _ := http.NewRequest("GET", "https://www.ittefaq.com.bd", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	defer resp.Body.Close()

	doc, _ := goquery.NewDocumentFromReader(resp.Body)
	var links []string
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if strings.Contains(href, ".bd/") && len(strings.Split(href, "/")) > 4 {
			links = append(links, href)
		} else if strings.HasPrefix(href, "/") && len(strings.Split(href, "/")) > 3 {
			links = append(links, "https://www.ittefaq.com.bd"+href)
		}
	})

	fmt.Printf("Ittefaq links found: %d\n", len(links))
	for i := 0; i < min(5, len(links)); i++ {
		fmt.Println("Link:", links[i])
		testIttefaqArticle(links[i])
	}
}

func testIttefaqArticle(url string) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return
	}
	title := doc.Find("h1").First().Text()
	img, _ := doc.Find(`meta[property="og:image"]`).Attr("content")
	fmt.Printf("  -> Title: %s\n  -> Img: %s\n", strings.TrimSpace(title), img)

	doc.Find("p").Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if len([]rune(t)) > 30 {
			parentClass, _ := s.Parent().Attr("class")
			fmt.Printf("  -> P [%s]: %s\n", parentClass, t[:min(60, len(t))])
		}
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
