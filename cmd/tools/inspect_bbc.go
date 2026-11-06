package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

func main() {
	cfg := config.Load()
	db, err := repository.NewPostgresDB(cfg)
	if err != nil {
		log.Fatal(err)
	}

	var a domain.Article
	db.Where("source_name = 'BBC News বাংলা'").Order("published_at DESC").First(&a)
	fmt.Printf("BBC Article: %s\nURL: %s\nContentLen: %d\n", a.Title, a.SourceURL, len(a.Content))

	req, _ := http.NewRequest("GET", a.SourceURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	defer resp.Body.Close()

	doc, _ := goquery.NewDocumentFromReader(resp.Body)
	var paras []string
	doc.Find("main p, [role='main'] p, article p").Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if len([]rune(t)) > 25 {
			paras = append(paras, t)
		}
	})

	fmt.Printf("Found %d paras, total length %d\n", len(paras), len(strings.Join(paras, "\n\n")))
	if len(paras) > 0 {
		fmt.Printf("First para: %s\n", paras[0])
	}
}
