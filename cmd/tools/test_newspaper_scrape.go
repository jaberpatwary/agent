package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func testScrapeNewspaper(name, siteURL, linkPrefix string) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", siteURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("❌ %s failed to load homepage: %v\n", name, err)
		return
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		fmt.Printf("❌ %s failed to parse HTML: %v\n", name, err)
		return
	}

	var articleLinks []string
	seen := make(map[string]bool)

	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		href = strings.TrimSpace(href)
		if strings.HasPrefix(href, "/") {
			// prepend domain
			domain := siteURL
			if strings.HasSuffix(domain, "/") {
				domain = domain[:len(domain)-1]
			}
			href = domain + href
		}
		if strings.Contains(href, linkPrefix) && !seen[href] && !strings.Contains(href, "#") {
			seen[href] = true
			articleLinks = append(articleLinks, href)
		}
	})

	fmt.Printf("\n=== %s ===\nFound %d article links from homepage!\n", name, len(articleLinks))
	if len(articleLinks) > 0 {
		testArticle(name, articleLinks[0])
	}
}

func testArticle(name, url string) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("   Failed article fetch: %v\n", err)
		return
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		fmt.Printf("   Failed article parse: %v\n", err)
		return
	}

	title := strings.TrimSpace(doc.Find("h1").First().Text())
	ogImg, _ := doc.Find(`meta[property="og:image"]`).Attr("content")
	if ogImg == "" {
		ogImg, _ = doc.Find(`meta[name="twitter:image"]`).Attr("content")
	}

	var paras []string
	doc.Find("article p, .story-element p, .content p, .article-content p, .dtl_content p, .details-content p, .description p, .news-details p").Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if len([]rune(t)) > 25 {
			paras = append(paras, t)
		}
	})

	fmt.Printf("   Article URL: %s\n", url)
	fmt.Printf("   Title: %s\n", title)
	fmt.Printf("   Image: %s\n", ogImg)
	fmt.Printf("   Paragraphs: %d (Total length: %d chars)\n", len(paras), len(strings.Join(paras, "\n\n")))
	if len(paras) > 0 {
		fmt.Printf("   Sample para: %s\n", paras[0])
	}
}

func main() {
	testScrapeNewspaper("Samakal", "https://samakal.com", "/article/")
	testScrapeNewspaper("Ittefaq", "https://www.ittefaq.com.bd", "/article/")
	testScrapeNewspaper("Jugantor", "https://www.jugantor.com", "/todays-paper/")
}
