package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	link := "https://news.google.com/rss/articles/CBMizwRBVV95cUxPN0lVbTBPcVlsUVZic19xbFZsNUlUMy1NNlBZTEZFbC0wcS1Ub2pNUjlocmZaYUdrQUNOYTZWdzZtM3MyZEtMWWFOUEZ3YzVsdURGR1JvM2NycnNvLTFmaTV3NXVRTzZIT0dnazhlb2dZV1prTG5DQVpVODRveWx4OWV3QVdyZHFXckNLN2FJdnhiMGVlM3Y0WW9fTVFsdTVnUGhEcHFsZEw2M2JQYkJjUkdtY2NCTHUzY0JCaFJCbk4teDdHWGhLV0VDaWJMbG5zS2FiTjliNkZoQ0Q5eHhtZmZZakhoZjlxLVdtTmk0bzFRVVFFSGl4T2p6bWdwWHVtTEU5cW1pNTlVVkJLREQ5T1RCWG9vYXdQQjJOemRBUkFlUUlRZVRpZzBLdUduYVBMQkZnZ0tYYlBJSTJ3NldjOERfanNxaVRTQnl0QlRTeVc5SGJtSzUzSFBnek9KYmVCZXYyUGw1cDZSYXBGeEdNRE5kLU1SSEp4azNjenlaNXlrMW5BOWRobEJiTk92c21rNHQzdmNqNE8xRTh3dExjeXpGLURicjlkZ1g3VWExV3h4eXRMS3BoMFJrdkhieXJkbmFLTGJmSUtpNEtOSVBpOWtRSEJLQXFtZWJ6ejJubnB3WWhsX292djZJXzltd1UxVkZVVm5LQ3pGOXA5NGpSOEozUEQ3LXpUX3VkVEVSSFFQUEZ2dGFvV2RXNkZpUzFfWE9vN1dvV09WS0JocGsyTGc5OHBaQWFvc1hWWG9jOHVIdWRyUVNjOVBaenlMV2s?oc=5"

	// Check base64
	idx := strings.Index(link, "/articles/")
	b64 := link[idx+10:]
	if q := strings.Index(b64, "?"); q != -1 {
		b64 = b64[:q]
	}
	dec, err := base64.RawURLEncoding.DecodeString(b64)
	if err == nil {
		fmt.Printf("Base64 decoded bytes len: %d\n", len(dec))
		// Search for http in decoded bytes
		strDec := string(dec)
		for _, part := range strings.Split(strDec, "http") {
			if len(part) > 10 {
				fmt.Printf("Found in protobuf: http%s\n", part[:min(len(part), 100)])
			}
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", link, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err == nil {
			ogTitle, _ := doc.Find(`meta[property="og:title"]`).Attr("content")
			ogImg, _ := doc.Find(`meta[property="og:image"]`).Attr("content")
			ogDesc, _ := doc.Find(`meta[property="og:description"]`).Attr("content")
			fmt.Printf("ogTitle: %s\n", ogTitle)
			fmt.Printf("ogImg: %s\n", ogImg)
			fmt.Printf("ogDesc: %s\n", ogDesc)

			// Look for real link in anchor
			doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
				href, _ := s.Attr("href")
				if strings.Contains(href, "samakal.com") || strings.Contains(href, "jugantor.com") || strings.Contains(href, "ittefaq.com.bd") {
					fmt.Printf("Found real anchor: %s\n", href)
				}
			})
			doc.Find("[data-n-a-u]").Each(func(i int, s *goquery.Selection) {
				href, _ := s.Attr("data-n-a-u")
				fmt.Printf("Found data-n-a-u: %s\n", href)
			})
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
