package main

import (
	"fmt"
	"log"

	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/repository"
)

func main() {
	cfg := config.Load()
	db, err := repository.NewPostgresDB(cfg)
	if err != nil {
		log.Fatalf("DB error: %v", err)
	}

	// Delete Jagonews articles (they're all empty - no image, no content)
	res := db.Exec("DELETE FROM articles WHERE source_name = 'জাগো নিউজ ২৪' AND (image_url = '' OR image_url IS NULL) AND length(content) < 100")
	fmt.Printf("Deleted %d empty জাগো নিউজ ২৪ articles\n", res.RowsAffected)

	// Deactivate Jagonews feed (blocks scraping with 403)
	res2 := db.Exec("UPDATE feed_sources SET active = false WHERE name = 'জাগো নিউজ ২৪'")
	fmt.Printf("Deactivated %d জাগো নিউজ ২৪ feeds\n", res2.RowsAffected)

	// Print current active feeds
	type FeedInfo struct {
		Name   string
		URL    string
		Active bool
	}
	var feeds []FeedInfo
	db.Raw("SELECT name, url, active FROM feed_sources WHERE active = true ORDER BY name").Scan(&feeds)
	fmt.Printf("\n✅ Active feeds (%d):\n", len(feeds))
	for _, f := range feeds {
		fmt.Printf("  - %s\n", f.Name)
	}

	// Article count per source
	type SourceStat struct {
		SourceName  string
		Total       int
		WithImg     int
		LongContent int
	}
	var stats []SourceStat
	db.Raw(`
		SELECT source_name, COUNT(*) as total,
		       COUNT(CASE WHEN image_url != '' AND image_url IS NOT NULL THEN 1 END) as with_img,
		       COUNT(CASE WHEN length(content) > 500 THEN 1 END) as long_content
		FROM articles
		GROUP BY source_name
		ORDER BY total DESC
	`).Scan(&stats)

	fmt.Println("\n📊 Article stats:")
	totalArt := 0
	for _, s := range stats {
		fmt.Printf("  %-28s Total: %4d  WithImg: %4d  LongContent: %4d\n", s.SourceName, s.Total, s.WithImg, s.LongContent)
		totalArt += s.Total
	}
	fmt.Printf("\n  Grand Total: %d articles\n", totalArt)
}
