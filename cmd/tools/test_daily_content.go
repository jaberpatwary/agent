package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	url := "https://daily-bangladesh.com/international/539643"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	defer resp.Body.Close()

	doc, _ := goquery.NewDocumentFromReader(resp.Body)
	var paras []string
	selectors := "article p, .story-element p, .content p, .article-content p, .dtl_content p, .details-content p, .description p, .news-details p, .jw-detail p, .dNewsDesc p, .entry-content p, .post-content p, .detail-box p, .details-text p, .content-details p, [itemprop='articleBody'] p, .story-body p, .article-body p"
	doc.Find(selectors).Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if len([]rune(t)) > 25 {
			paras = append(paras, t)
		}
	})

	fmt.Printf("Found %d paragraphs! Total length: %d chars\n", len(paras), len(strings.Join(paras, "\n\n")))
	for i, p := range paras {
		fmt.Printf("[%d] %s\n\n", i+1, p)
	}
}
