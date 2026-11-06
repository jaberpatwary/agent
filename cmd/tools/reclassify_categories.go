// Command reclassify_categories reapplies headline-based categories to published stories.
package main

import (
	"log"
	"strings"

	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/cron"
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
	if err := db.Where("status = ?", "published").Find(&articles).Error; err != nil {
		log.Fatal(err)
	}
	updated := 0
	for _, article := range articles {
		category := cron.ClassifyCategory(article.Title, "", sourceDefaultCategory(article.SourceName))
		if category == article.Category {
			continue
		}
		if err := db.Model(&domain.Article{}).Where("id = ?", article.ID).Update("category", category).Error; err != nil {
			log.Printf("could not update %q: %v", article.Title, err)
			continue
		}
		updated++
	}
	log.Printf("Reclassified %d of %d published stories from their headlines.", updated, len(articles))
}

func sourceDefaultCategory(sourceName string) string {
	name := strings.ToLower(sourceName)
	switch {
	case strings.Contains(name, "techshohor"), strings.Contains(name, "টেকশহর"):
		return "technology"
	case strings.Contains(name, "bbc"), strings.Contains(name, "বিবিসি"), strings.Contains(name, "deutsche welle"), strings.Contains(name, "ডয়েচে ভেলে"):
		return "international"
	case strings.Contains(name, "prothom alo"), strings.Contains(name, "risingbd"), strings.Contains(name, "bd24live"):
		return "bangladesh"
	default:
		return "general"
	}
}
