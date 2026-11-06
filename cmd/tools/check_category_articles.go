package main

import (
	"fmt"
	"log"

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

	var articles []domain.Article
	db.Where("category = ? AND status = 'published'", "technology").Order("published_at DESC").Limit(15).Find(&articles)

	fmt.Printf("Technology articles count: %d\n", len(articles))
	for i, a := range articles {
		fmt.Printf("[%d] %s | Source: %s | Img: %s | ContentLen: %d | URL: %s\n",
			i+1, a.Title, a.SourceName, a.ImageURL != "", len(a.Content), a.SourceURL)
	}

	var latestArticles []domain.Article
	db.Where("status = 'published'").Order("published_at DESC").Limit(20).Find(&latestArticles)
	fmt.Printf("\nLatest articles overall (first page):\n")
	for i, a := range latestArticles {
		hasImg := "❌ NO IMG"
		if a.ImageURL != "" {
			hasImg = "📸 IMG OK"
		}
		fmt.Printf("[%d] [%s] (Len: %4d) %s | %s\n", i+1, hasImg, len(a.Content), a.SourceName, a.Title)
	}
}
