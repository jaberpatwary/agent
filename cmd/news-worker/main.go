package main

import (
	"context"
	"log"
	"time"

	"bangla-news-aggregator/internal/agentic"
	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/repository"
	"bangla-news-aggregator/internal/rss"
)

// news-worker runs one pass of the agentic news desk and exits. It is
// intended for scheduled runners such as GitHub Actions, separate from the
// web process (which can also be triggered by n8n at /api/agent/run).
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

	feedRepo := repository.NewFeedRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	journeyRepo := repository.NewJourneyRepository(db)
	worker := cron.NewWorker(
		cfg,
		feedRepo,
		repository.NewArticleRepository(db),
		repository.NewAIUsageRepository(db),
		settingsRepo,
		repository.NewSeenURLRepository(db),
	)
	worker.SetJourneyRepo(journeyRepo)
	worker.SetPopularRepo(repository.NewPopularRepository(db))

	desk := agentic.NewDesk(worker, repository.NewAgentRepository(db), journeyRepo, settingsRepo, feedRepo,
		agentic.GoogleNewsSearch(nil), agentic.PageReader(rss.NewClient()))

	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	run, err := desk.RunOnce(ctx, "news-worker")
	if err != nil {
		log.Fatalf("[news-worker] %v", err)
	}
	log.Printf("[news-worker] agent run %s: status=%s processed=%d/%d tokens=%d %s",
		run.ID, run.Status, run.Processed, run.Candidates, run.Tokens, run.Error)
}
