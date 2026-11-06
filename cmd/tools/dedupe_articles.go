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
		log.Fatal(err)
	}

	res := db.Exec(`
		DELETE FROM articles a
		USING articles b
		WHERE a.id < b.id 
		  AND (a.source_url = b.source_url OR a.title = b.title)
	`)
	fmt.Printf("Cleaned %d duplicate articles\n", res.RowsAffected)
}
