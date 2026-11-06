package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

func main() {
	link := "https://news.google.com/rss/articles/CBMizwRBVV95cUxPN0lVbTBPcVlsUVZic19xbFZsNUlUMy1NNlBZTEZFbC0wcS1Ub2pNUjlocmZaYUdrQUNOYTZWdzZtM3MyZEtMWWFOUEZ3YzVsdURGR1JvM2NycnNvLTFmaTV3NXVRTzZIT0dnazhlb2dZV1prTG5DQVpVODRveWx4OWV3QVdyZHFXckNLN2FJdnhiMGVlM3Y0WW9fTVFsdTVnUGhEcHFsZEw2M2JQYkJjUkdtY2NCTHUzY0JCaFJCbk4teDdHWGhLV0VDaWJMbG5zS2FiTjliNkZoQ0Q5eHhtZmZZakhoZjlxLVdtTmk0bzFRVVFFSGl4T2p6bWdwWHVtTEU5cW1pNTlVVkJLREQ5T1RCWG9vYXdQQjJOemRBUkFlUUlRZVRpZzBLdUduYVBMQkZnZ0tYYlBJSTJ3NldjOERfanNxaVRTQnl0QlRTeVc5SGJtSzUzSFBnek9KYmVCZXYyUGw1cDZSYXBGeEdNRE5kLU1SSEp4azNjenlaNXlrMW5BOWRobEJiTk92c21rNHQzdmNqNE8xRTh3dExjeXpGLURicjlkZ1g3VWExV3h4eXRMS3BoMFJrdkhieXJkbmFLTGJmSUtpNEtOSVBpOWtRSEJLQXFtZWJ6ejJubnB3WWhsX292djZJXzltd1UxVkZVVm5LQ3pGOXA5NGpSOEozUEQ3LXpUX3VkVEVSSFFQUEZ2dGFvV2RXNkZpUzFfWE9vN1dvV09WS0JocGsyTGc5OHBaQWFvc1hWWG9jOHVIdWRyUVNjOVBaenlMV2s?oc=5"

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", link, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Search for samakal or http
	re := regexp.MustCompile(`"(https?://[^"]+)"`)
	matches := re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		u := m[1]
		if strings.Contains(u, "samakal.com") || strings.Contains(u, "jugantor.com") || strings.Contains(u, "ittefaq.com.bd") || strings.Contains(u, "kalbela.com") || strings.Contains(u, "mzamin.com") {
			fmt.Printf("TARGET REAL URL FOUND: %s\n", u)
		}
	}

	// Also check for data-n-au or c-wiz
	re2 := regexp.MustCompile(`data-n-au="([^"]+)"`)
	m2 := re2.FindStringSubmatch(html)
	if len(m2) > 1 {
		fmt.Printf("data-n-au: %s\n", m2[1])
	}
}
