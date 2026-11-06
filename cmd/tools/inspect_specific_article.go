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

	var article domain.Article
	db.Where("id = ?", "fcfb98ec-61c4-44a3-b933-dc8c91453978").First(&article)
	fmt.Printf("ID: %s\nTitle: %s\nSource: %s\nURL: %s\nImg: %s\nContentLen: %d\nContent: %s\n",
		article.ID, article.Title, article.SourceName, article.SourceURL, article.ImageURL, len(article.Content), article.Content)
}
