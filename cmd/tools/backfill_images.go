// Command backfill_images enriches existing published stories that have no
// thumbnail or only a short RSS excerpt.
package main

import (
	"context"
	"fmt"
	"html"
	"log"
	"regexp"
	"strings"
	"sync"

	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
	"bangla-news-aggregator/internal/rss"
)

var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

func main() {
	cfg := config.Load()
	db, err := repository.NewPostgresDB(cfg)
	if err != nil {
		log.Fatal(err)
	}
	var articles []domain.Article
	if err := db.Where("status = ?", "published").Find(&articles).Error; err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Found %d published stories to enrich.\n", len(articles))

	client := rss.NewClient()
	articleByURL := make(map[string]domain.Article, len(articles))
	articleByTitle := make(map[string]domain.Article, len(articles))
	bodyCounts := make(map[string]int)
	for _, article := range articles {
		articleByURL[article.SourceURL] = article
		articleByTitle[strings.TrimSpace(article.Title)] = article
		bodyCounts[strings.TrimSpace(article.Content)]++
	}
	feeds, err := repository.NewFeedRepository(db).ListActive()
	if err != nil {
		log.Printf("could not load feeds for RSS matching: %v", err)
	}
	updatedImages, updatedBodies, failed := 0, 0, 0
	matchedFeedArticles := make(map[string]bool)
	for _, feed := range feeds {
		items, fetchErr := client.Fetch(context.Background(), feed.URL)
		if fetchErr != nil {
			log.Printf("could not scan feed %q: %v", feed.Name, fetchErr)
			continue
		}
		for _, item := range items {
			article, ok := articleByURL[item.OriginalLink]
			if !ok {
				article, ok = articleByURL[item.Link]
			}
			if !ok {
				// A previous run may have replaced a Google story URL with the
				// publisher homepage. Match that orphaned record by its title.
				title := strings.TrimSpace(strings.TrimSuffix(item.Title, " - "+item.PublisherName))
				article, ok = articleByTitle[title]
			}
			if !ok {
				continue
			}
			updates := make(map[string]interface{})
			if item.PublisherName != "" && item.PublisherName != article.SourceName {
				updates["source_name"] = item.PublisherName
			}
			if item.Link != "" && item.Link != article.SourceURL && !strings.Contains(item.Link, "news.google.com") {
				var duplicateCount int64
				if err := db.Model(&domain.Article{}).Where("source_url = ? AND id <> ?", item.Link, article.ID).Count(&duplicateCount).Error; err == nil && duplicateCount == 0 {
					updates["source_url"] = item.Link
				}
			}
			if strings.TrimSpace(article.ImageURL) == "" && item.ImageURL != "" {
				updates["image_url"] = item.ImageURL
			}
			oldVisibleLength := len(strings.TrimSpace(htmlTagPattern.ReplaceAllString(html.UnescapeString(article.Content), "")))
			if len(item.Content) >= 300 && (len(item.Content) > oldVisibleLength || bodyCounts[strings.TrimSpace(article.Content)] > 1) {
				updates["content"] = item.Content
			}
			category := cron.ClassifyCategory(article.Title, item.Content+" "+article.Summary, "general")
			if category != article.Category {
				updates["category"] = category
			}
			if len(updates) == 0 {
				matchedFeedArticles[article.SourceURL] = true
				continue
			}
			if err := db.Model(&domain.Article{}).Where("id = ?", article.ID).Updates(updates).Error; err != nil {
				failed++
				log.Printf("failed to apply RSS enrichment for article %s: %v", article.ID, err)
				continue
			}
			if _, ok := updates["image_url"]; ok {
				updatedImages++
			}
			if _, ok := updates["content"]; ok {
				updatedBodies++
			}
			matchedFeedArticles[article.SourceURL] = true
			if newURL, ok := updates["source_url"].(string); ok {
				article.SourceURL = newURL
				articleByURL[newURL] = article
			}
			articleByURL[item.OriginalLink] = article
			articleByURL[item.Link] = article
		}
	}
	fmt.Printf("Matched %d existing stories to their RSS entries.\n", len(matchedFeedArticles))

	jobs := make(chan domain.Article)
	var wg sync.WaitGroup
	var mu sync.Mutex
	workerCount := 4
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for article := range jobs {
				updates := make(map[string]interface{})
				category := cron.ClassifyCategory(article.Title, article.Content+" "+article.Summary, "general")
				if category != article.Category {
					updates["category"] = category
				}
				visibleLength := len(strings.TrimSpace(htmlTagPattern.ReplaceAllString(html.UnescapeString(article.Content), "")))
				if !matchedFeedArticles[article.SourceURL] && (strings.TrimSpace(article.ImageURL) == "" || visibleLength < 1500) && !strings.Contains(article.SourceURL, "news.google.com") {
					_, imageURL, content := client.Enrich(context.Background(), article.SourceURL)
					if strings.TrimSpace(article.ImageURL) == "" && imageURL != "" {
						updates["image_url"] = imageURL
					}
					if len(content) > visibleLength && len(content) >= 300 {
						updates["content"] = content
					}
				}
				if len(updates) == 0 {
					continue
				}
				if err := db.Model(&domain.Article{}).Where("id = ?", article.ID).Updates(updates).Error; err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					log.Printf("failed to update article %s: %v", article.ID, err)
					continue
				}
				mu.Lock()
				if _, ok := updates["image_url"]; ok {
					updatedImages++
				}
				if _, ok := updates["content"]; ok {
					updatedBodies++
				}
				mu.Unlock()
			}
		}()
	}
	for _, article := range articles {
		jobs <- article
	}
	close(jobs)
	wg.Wait()
	fmt.Printf("Backfill finished: images added=%d, full bodies improved=%d, database errors=%d.\n", updatedImages, updatedBodies, failed)
}
