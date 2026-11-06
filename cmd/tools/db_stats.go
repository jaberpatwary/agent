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
	var feeds []domain.FeedSource
	db.Find(&feeds)
	fmt.Printf("Total feeds: %d\n", len(feeds))
	for _, f := range feeds {
		fmt.Printf("- ID: %d, Name: %s, Active: %v, URL: %s\n", f.ID, f.Name, f.Active, f.URL)
	}

	type SourceCount struct {
		SourceName  string
		Total       int
		WithImg     int
		LongContent int
	}
	var stats []SourceCount
	db.Raw(`
		SELECT source_name, 
		       COUNT(*) as total, 
		       COUNT(CASE WHEN image_url != '' AND image_url IS NOT NULL THEN 1 END) as with_img,
		       COUNT(CASE WHEN length(content) > 300 THEN 1 END) as long_content
		FROM articles 
		GROUP BY source_name
	`).Scan(&stats)

	fmt.Println("\nArticle stats by source:")
	for _, s := range stats {
		fmt.Printf("Source: %-25s Total: %4d WithImg: %4d LongContent: %4d\n", s.SourceName, s.Total, s.WithImg, s.LongContent)
	}
}
