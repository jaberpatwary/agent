package main

import (
	"context"
	"fmt"
	"log"

	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

func main() {
	cfg := config.Load()
	db, err := repository.NewPostgresDB(cfg)
	if err != nil {
		log.Fatalf("db error: %v", err)
	}

	feedRepo := repository.NewFeedRepository(db)
	articleRepo := repository.NewArticleRepository(db)
	usageRepo := repository.NewAIUsageRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)

	qualityFeeds := []domain.FeedSource{
		{
			Name:     "প্রথম আলো",
			Category: "general",
			URL:      "https://www.prothomalo.com/feed",
			Active:   true,
		},
		{
			Name:     "এনটিভি অনলাইন",
			Category: "general",
			URL:      "https://www.ntvbd.com/rss.xml",
			Active:   true,
		},
		{
			Name:     "বিবিসি বাংলা",
			Category: "international",
			URL:      "https://feeds.bbci.co.uk/bengali/rss.xml",
			Active:   true,
		},
		{
			Name:     "রাইজিংবিডি",
			Category: "general",
			URL:      "https://www.risingbd.com/rss",
			Active:   true,
		},
		{
			Name:     "ডেইলি বাংলাদেশ",
			Category: "general",
			URL:      "https://www.daily-bangladesh.com/feed",
			Active:   true,
		},
		{
			Name:     "টেকশহর",
			Category: "technology",
			URL:      "https://techshohor.com/feed",
			Active:   true,
		},
		{
			Name:     "চ্যানেল আই অনলাইন",
			Category: "general",
			URL:      "https://www.channelionline.com/feed",
			Active:   true,
		},
		{
			Name:     "ডয়চে ভেলে বাংলা",
			Category: "international",
			URL:      "https://rss.dw.com/xml/rss-ben-all",
			Active:   true,
		},
	}

	// Deactivate unwanted / broken Google News & imageless feeds
	db.Exec("UPDATE feed_sources SET active = false WHERE url LIKE '%news.google.com%' OR url LIKE '%jagonews24.com%'")

	for _, f := range qualityFeeds {
		var existing domain.FeedSource
		res := db.Where("url = ?", f.URL).First(&existing)
		if res.Error != nil {
			if err := feedRepo.Create(&f); err != nil {
				log.Printf("Failed to insert %s: %v", f.Name, err)
			} else {
				fmt.Printf("✅ Added quality feed: %s (%s)\n", f.Name, f.URL)
			}
		} else {
			existing.Active = true
			existing.Category = f.Category
			db.Save(&existing)
			fmt.Printf("ℹ️ Updated feed: %s (%s)\n", existing.Name, existing.URL)
		}
	}

	// Clean out stub articles from google news & imageless stubs
	res := db.Exec("DELETE FROM articles WHERE source_url LIKE '%news.google.com%' OR ((image_url = '' OR image_url IS NULL) AND source_name IN ('জাগো নিউজ ২৪', 'Google News বাংলা (Bangladesh)'))")
	fmt.Printf("🧹 Removed %d incomplete stub articles with no pictures\n", res.RowsAffected)

	fmt.Println("\n🚀 Running Worker with upgraded image & full text extractor...")
	worker := cron.NewWorker(cfg, feedRepo, articleRepo, usageRepo, settingsRepo, repository.NewSeenURLRepository(db))
	worker.RunOnce(context.Background())
	fmt.Println("🎉 All done!")
}
