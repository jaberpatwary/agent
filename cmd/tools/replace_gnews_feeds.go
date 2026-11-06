package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"

	"github.com/mmcdole/gofeed"
)

func tryFeed(name, url string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/rss+xml, application/xml, */*")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("  ❌ %s: %v\n", url, err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Printf("  ❌ %s: HTTP %d\n", url, resp.StatusCode)
		return false
	}
	fp := gofeed.NewParser()
	feed, err := fp.Parse(resp.Body)
	if err != nil || len(feed.Items) == 0 {
		fmt.Printf("  ❌ %s: %v\n", url, err)
		return false
	}
	hasImg := "no-img"
	if feed.Items[0].Image != nil && feed.Items[0].Image.URL != "" {
		hasImg = "✅img"
	} else if len(feed.Items[0].Enclosures) > 0 {
		hasImg = "enc"
	}
	fmt.Printf("  ✅ %s (%d items, %s, title=%s)\n", url, len(feed.Items), hasImg, feed.Items[0].Title)
	return true
}

func main() {
	cfg := config.Load()
	db, err := repository.NewPostgresDB(cfg)
	if err != nil {
		log.Fatalf("DB error: %v", err)
	}
	feedRepo := repository.NewFeedRepository(db)
	articleRepo := repository.NewArticleRepository(db)
	usageRepo := repository.NewAIUsageRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)

	// 1. Delete all The Daily Star বাংলা and remaining জাগো নিউজ articles (from Google News = no image/content)
	res1 := db.Exec("DELETE FROM articles WHERE source_name = 'The Daily Star বাংলা'")
	fmt.Printf("Deleted %d The Daily Star বাংলা articles\n", res1.RowsAffected)

	res2 := db.Exec("DELETE FROM articles WHERE source_name = 'জাগো নিউজ ২৪'")
	fmt.Printf("Deleted %d remaining জাগো নিউজ ২৪ articles\n", res2.RowsAffected)

	// 2. Deactivate Google News based feeds
	res3 := db.Exec("UPDATE feed_sources SET active = false WHERE url LIKE '%news.google.com%'")
	fmt.Printf("Deactivated %d Google News feeds\n", res3.RowsAffected)

	// 3. Try to find a working direct feed for Daily Star Bangla
	fmt.Println("\nLooking for The Daily Star Bangla direct feed...")
	dsFeeds := []string{
		"https://bangla.thedailystar.net/rss.xml",
		"https://bangla.thedailystar.net/feed",
		"https://bangla.thedailystar.net/rss",
	}
	var dsURL string
	for _, u := range dsFeeds {
		if tryFeed("Daily Star Bangla", u) {
			dsURL = u
			break
		}
	}

	// 4. Add more feeds that were found working from sitemap scraper
	newFeeds := []domain.FeedSource{}

	if dsURL != "" {
		newFeeds = append(newFeeds, domain.FeedSource{
			Name:     "দ্য ডেইলি স্টার বাংলা",
			Category: "general",
			URL:      dsURL,
			Active:   true,
		})
	}

	// Try Kaler Kantho direct  
	fmt.Println("\nLooking for কালের কণ্ঠ direct feed...")
	kkFeeds := []string{"https://www.kalerkantho.com/feed", "https://www.kalerkantho.com/rss.xml", "https://www.kalerkantho.com/online/feed"}
	for _, u := range kkFeeds {
		if tryFeed("কালের কণ্ঠ", u) {
			newFeeds = append(newFeeds, domain.FeedSource{Name: "দৈনিক কালের কণ্ঠ", Category: "general", URL: u, Active: true})
			break
		}
	}

	// Try Bangla Tribune direct
	fmt.Println("\nLooking for বাংলা ট্রিবিউন direct feed...")
	btFeeds := []string{"https://www.banglatribune.com/feed", "https://banglatribune.com/rss.xml"}
	for _, u := range btFeeds {
		if tryFeed("বাংলা ট্রিবিউন", u) {
			newFeeds = append(newFeeds, domain.FeedSource{Name: "বাংলা ট্রিবিউন", Category: "general", URL: u, Active: true})
			break
		}
	}

	// Try Manabzamin direct
	fmt.Println("\nLooking for মানবজমিন direct feed...")
	mzFeeds := []string{"https://mzamin.com/rss.xml", "https://www.mzamin.com/rss.xml", "https://mzamin.com/feed"}
	for _, u := range mzFeeds {
		if tryFeed("মানবজমিন", u) {
			newFeeds = append(newFeeds, domain.FeedSource{Name: "দৈনিক মানবজমিন", Category: "general", URL: u, Active: true})
			break
		}
	}

	// 5. Add working feeds to DB
	added := 0
	for i, f := range newFeeds {
		var existing domain.FeedSource
		if db.Where("url = ?", f.URL).First(&existing).Error != nil {
			if err := feedRepo.Create(&newFeeds[i]); err != nil {
				log.Printf("Failed to add %s: %v", f.Name, err)
			} else {
				fmt.Printf("✅ Added feed: %s\n", f.Name)
				added++
			}
		} else {
			existing.Active = true
			db.Save(&existing)
			fmt.Printf("🔄 Activated: %s\n", existing.Name)
			added++
		}
	}
	fmt.Printf("\n%d feeds added/activated\n", added)

	// 6. Run the worker
	if added > 0 {
		fmt.Println("\n🚀 Running worker to fetch new articles...")
		worker := cron.NewWorker(cfg, feedRepo, articleRepo, usageRepo, settingsRepo, repository.NewSeenURLRepository(db))
		worker.RunOnce(context.Background())
	}
	fmt.Println("🎉 Done!")
}
