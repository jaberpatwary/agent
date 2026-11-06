package main

import (
	"context"
	"log"
	"time"

	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/repository"
)

// news-worker performs one feed polling pass and exits. It is intended for
// scheduled runners such as GitHub Actions, separate from the web process.
func main() {
	cfg := config.Load()

	db, err := repository.OpenPostgresDB(cfg)
	if err != nil {
		log.Fatalf("[news-worker] database connection failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("[news-worker] could not open database connection: %v", err)
	}
	defer sqlDB.Close()

	worker := cron.NewWorker(
		cfg,
		repository.NewFeedRepository(db),
		repository.NewArticleRepository(db),
		repository.NewAIUsageRepository(db),
		repository.NewSettingsRepository(db),
		repository.NewSeenURLRepository(db),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	worker.RunOnce(ctx)
	if err := ctx.Err(); err != nil && err != context.Canceled {
		log.Fatalf("[news-worker] run did not finish: %v", err)
	}
	log.Println("[news-worker] scheduled feed pass completed")
}
