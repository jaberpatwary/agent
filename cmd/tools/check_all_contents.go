package main

import (
	"fmt"
	"log"
	"strings"

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
	db.Where("status = 'published'").Order("published_at DESC").Limit(30).Find(&articles)

	fmt.Println("Top 30 Latest Articles:")
	for i, a := range articles {
		lines := strings.Count(a.Content, "<p>")
		fmt.Printf("[%2d] (Len: %5d, <p> tags: %2d) [%s] %s\n", i+1, len(a.Content), lines, a.SourceName, a.Title)
		if len(a.Content) < 500 {
			fmt.Printf("     Preview: %s\n", strings.ReplaceAll(a.Content, "\n", " "))
		}
	}
}
