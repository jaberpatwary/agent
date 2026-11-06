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
	db.Where("status = 'published'").Order("published_at DESC").Limit(30).Find(&articles)

	fmt.Println("Top 30 Articles currently shown on homepage:")
	for i, a := range articles {
		fmt.Printf("[%2d] Published: %s | Source: %-22s | Title: %s\n",
			i+1, a.PublishedAt.Format("2006-01-02 15:04:05"), a.SourceName, a.Title)
	}

	type SourceStats struct {
		SourceName string
		Count      int
		LatestPub  string
	}
	var stats []SourceStats
	db.Raw(`
		SELECT source_name, COUNT(*) as count, MAX(published_at)::text as latest_pub
		FROM articles
		WHERE status = 'published'
		GROUP BY source_name
		ORDER BY latest_pub DESC
	`).Scan(&stats)

	fmt.Println("\nLatest published time by Source:")
	for _, s := range stats {
		fmt.Printf("Source: %-25s | Count: %4d | Latest: %s\n", s.SourceName, s.Count, s.LatestPub)
	}
}
