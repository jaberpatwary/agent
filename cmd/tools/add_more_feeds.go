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

type feedCandidate struct {
	name     string
	category string
	urls     []string
}

func tryFeed(name, url string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, */*")

	client := &http.Client{
		Timeout: 12 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("  ❌ %s => %s: %v\n", name, url, err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Printf("  ❌ %s => %s: HTTP %d\n", name, url, resp.StatusCode)
		return false
	}

	fp := gofeed.NewParser()
	feed, err := fp.Parse(resp.Body)
	if err != nil {
		fmt.Printf("  ❌ %s => %s: parse error: %v\n", name, url, err)
		return false
	}

	if len(feed.Items) == 0 {
		fmt.Printf("  ⚠️  %s => %s: 0 items\n", name, url)
		return false
	}

	hasImg := "no-img"
	if feed.Items[0].Image != nil && feed.Items[0].Image.URL != "" {
		hasImg = "has-img"
	} else if len(feed.Items[0].Enclosures) > 0 {
		hasImg = "has-enc"
	}
	fmt.Printf("  ✅ %s => %s (%d items, %s)\n", name, url, len(feed.Items), hasImg)
	return true
}

func main() {
	candidates := []feedCandidate{
		// These had wrong URLs - trying corrected versions
		{name: "দৈনিক সমকাল", category: "general", urls: []string{
			"https://samakal.com/rss.xml",
			"https://samakal.com/feed/",
			"https://samakal.com/feed",
			"https://www.samakal.com/feed",
		}},
		{name: "দৈনিক যুগান্তর", category: "general", urls: []string{
			"https://www.jugantor.com/feed/rss.xml",
			"https://www.jugantor.com/feed",
			"https://jugantor.com/feed",
		}},
		{name: "দৈনিক ইত্তেফাক", category: "general", urls: []string{
			"https://www.ittefaq.com.bd/feed/",
			"https://www.ittefaq.com.bd/feed",
			"https://ittefaq.com.bd/feed",
		}},
		{name: "দৈনিক কালবেলা", category: "general", urls: []string{
			"https://www.kalbela.com/feed/",
			"https://www.kalbela.com/feed",
			"https://kalbela.com/feed",
			"https://www.kalbela.com/rss",
		}},
		{name: "দৈনিক মানবজমিন", category: "general", urls: []string{
			"https://mzamin.com/rss.xml",
			"https://www.mzamin.com/feed",
			"https://mzamin.com/feed",
		}},
		{name: "দৈনিক ইনকিলাব", category: "general", urls: []string{
			"https://www.dailyinqilab.com/feed/",
			"https://www.dailyinqilab.com/feed",
			"https://dailyinqilab.com/rss.xml",
		}},
		{name: "দৈনিক কালের কণ্ঠ", category: "general", urls: []string{
			"https://www.kalerkantho.com/rss.xml",
			"https://www.kalerkantho.com/feed",
			"https://kalerkantho.com/feed",
		}},
		{name: "বাংলা ট্রিবিউন", category: "general", urls: []string{
			"https://www.banglatribune.com/feed",
			"https://banglatribune.com/feed",
			"https://www.banglatribune.com/rss.xml",
		}},
		{name: "ঢাকা পোস্ট", category: "general", urls: []string{
			"https://www.dhakapost.com/rss",
			"https://www.dhakapost.com/feed",
			"https://dhakapost.com/rss",
		}},
		{name: "বার্তা২৪", category: "general", urls: []string{
			"https://barta24.com/rss.xml",
			"https://www.barta24.com/feed",
			"https://barta24.com/feed",
		}},
		{name: "দেশ রূপান্তর", category: "general", urls: []string{
			"https://www.deshrupantor.com/feed",
			"https://deshrupantor.com/feed",
			"https://www.deshrupantor.com/rss.xml",
		}},
		{name: "ভোরের কাগজ", category: "general", urls: []string{
			"https://www.bhorerkagoj.com/feed",
			"https://bhorerkagoj.com/feed",
		}},
		{name: "সময় নিউজ", category: "general", urls: []string{
			"https://somoynews.tv/rss.xml",
			"https://www.somoynews.tv/rss.xml",
			"https://somoynews.tv/feed",
		}},
		{name: "বিডিনিউজ২৪ বাংলা", category: "general", urls: []string{
			"https://bangla.bdnews24.com/rss",
			"https://bangla.bdnews24.com/feed",
		}},
		{name: "আমাদের সময়", category: "general", urls: []string{
			"https://www.amadershomoy.com/rss.xml",
			"https://amadershomoy.com/feed",
			"https://dainikamadershomoy.com/rss.xml",
		}},
		{name: "দৈনিক জনকণ্ঠ", category: "general", urls: []string{
			"https://www.dailyjanakantha.com/rss.xml",
			"https://dailyjanakantha.com/feed",
		}},
		{name: "বণিক বার্তা", category: "business", urls: []string{
			"https://bonikbarta.net/feed",
			"https://bonikbarta.net/rss",
			"https://www.bonikbarta.com/feed",
		}},
	}

	fmt.Println("=== Testing all candidate feeds ===")
	type working struct {
		name     string
		category string
		url      string
	}
	var workingFeeds []working

	for _, c := range candidates {
		found := false
		for _, u := range c.urls {
			if tryFeed(c.name, u) {
				workingFeeds = append(workingFeeds, working{c.name, c.category, u})
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("  ⛔ %s: no working feed found\n", c.name)
		}
	}

	fmt.Printf("\n=== %d working feeds found ===\n", len(workingFeeds))
	for _, f := range workingFeeds {
		fmt.Printf("  → %s: %s\n", f.name, f.url)
	}

	if len(workingFeeds) == 0 {
		fmt.Println("No new feeds to add. Exiting.")
		return
	}

	cfg := config.Load()
	db, err := repository.NewPostgresDB(cfg)
	if err != nil {
		log.Fatalf("DB error: %v", err)
	}
	feedRepo := repository.NewFeedRepository(db)
	articleRepo := repository.NewArticleRepository(db)
	usageRepo := repository.NewAIUsageRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)

	added := 0
	for _, f := range workingFeeds {
		// Deactivate old google-news version if any
		db.Exec("UPDATE feed_sources SET active = false WHERE name = ? AND url LIKE '%news.google.com%'", f.name)

		var existing domain.FeedSource
		res := db.Where("url = ?", f.url).First(&existing)
		if res.Error != nil {
			newFeed := &domain.FeedSource{
				Name:     f.name,
				Category: f.category,
				URL:      f.url,
				Active:   true,
			}
			if err := feedRepo.Create(newFeed); err != nil {
				log.Printf("Failed to add %s: %v", f.name, err)
			} else {
				fmt.Printf("✅ Added: %s\n", f.name)
				added++
			}
		} else {
			existing.Active = true
			existing.Category = f.category
			db.Save(&existing)
			fmt.Printf("🔄 Activated existing: %s\n", f.name)
			added++
		}
	}

	fmt.Printf("\n✅ %d feeds added/activated\n", added)
	fmt.Println("\n🚀 Running worker to fetch new articles from all feeds...")
	worker := cron.NewWorker(cfg, feedRepo, articleRepo, usageRepo, settingsRepo, repository.NewSeenURLRepository(db))
	worker.RunOnce(context.Background())
	fmt.Println("🎉 Done!")
}
