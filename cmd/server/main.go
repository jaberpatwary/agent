package main

import (
	"context"
	"crypto/subtle"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"bangla-news-aggregator/internal/agentic"
	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/handler"
	"bangla-news-aggregator/internal/repository"
	"bangla-news-aggregator/internal/rss"
	"bangla-news-aggregator/internal/usecase"
)

func main() {
	cfg := config.Load()

	db, err := repository.NewPostgresDB(cfg)
	if err != nil {
		log.Fatalf("[main] database connection failed: %v", err)
	}
	log.Println("[main] connected to Postgres and ran migrations")

	// ---- Repositories ----
	articleRepo := repository.NewArticleRepository(db)
	feedRepo := repository.NewFeedRepository(db)
	adminRepo := repository.NewAdminRepository(db)
	usageRepo := repository.NewAIUsageRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)

	// ---- Usecases ----
	authUC := usecase.NewAuthUsecase(adminRepo, cfg)
	articleUC := usecase.NewArticleUsecase(articleRepo)
	feedUC := usecase.NewFeedUsecase(feedRepo)
	settingsUC := usecase.NewSettingsUsecase(settingsRepo)
	usageUC := usecase.NewAIUsageUsecase(usageRepo)

	// ---- Cron worker (RSS -> agentic AI rewrite -> DB pipeline) ----
	worker := cron.NewWorker(cfg, feedRepo, articleRepo, usageRepo, settingsRepo, repository.NewSeenURLRepository(db))
	worker.SetJourneyRepo(repository.NewJourneyRepository(db))

	cronCtx, cancelCron := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	if strings.EqualFold(strings.TrimSpace(os.Getenv("DISABLE_BACKGROUND_WORKER")), "true") {
		close(workerDone)
		log.Println("[cron] background worker disabled by configuration")
	} else {
		go func() {
			defer close(workerDone)
			worker.Start(cronCtx)
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	// ---- HTTP handlers & router ----
	publicH := handler.NewPublicHandler(articleUC, cfg.PublicBaseURL)
	adminH := handler.NewAdminHandler(authUC, articleUC, feedUC, settingsUC, usageUC, worker, cfg.JWTExpiry)
	// ---- Agentic news desk (see internal/agentic) ----
	journeyRepo := repository.NewJourneyRepository(db)
	desk := agentic.NewDesk(worker, repository.NewAgentRepository(db), journeyRepo, settingsRepo, feedRepo,
		agentic.GoogleNewsSearch(nil), agentic.PageReader(rss.NewClient()))
	adminH.SetAgent(desk, journeyRepo)

	var n8nH *handler.N8NHandler
	if cfg.N8NAPIKey != "" {
		n8nH = handler.NewN8NHandler(worker, cfg.N8NAPIKey)
		n8nH.SetDesk(desk)
		log.Println("[main] n8n API enabled at /api/n8n and /api/agent")
	}
	router := handler.NewRouter(authUC, publicH, adminH, n8nH, cfg.ImageStorageDir)
	if controlToken := os.Getenv("AUTONEWS_CONTROL_TOKEN"); controlToken != "" {
		router.POST("/_control/shutdown", func(c *gin.Context) {
			host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
			ip := net.ParseIP(host)
			provided := c.GetHeader("X-Autonews-Control")
			if err != nil || ip == nil || !ip.IsLoopback() ||
				subtle.ConstantTimeCompare([]byte(provided), []byte(controlToken)) != 1 {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			c.JSON(http.StatusAccepted, gin.H{"status": "shutting down"})
			go func() { quit <- syscall.SIGTERM }()
		})
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		log.Printf("[main] server listening on :%s (env=%s)", cfg.Port, cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[main] server failed: %v", err)
		}
	}()

	// ---- Graceful shutdown ----
	<-quit

	log.Println("[main] shutting down gracefully...")
	cancelCron()
	select {
	case <-workerDone:
	case <-time.After(20 * time.Second):
		log.Println("[main] feed worker shutdown timed out")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("[main] forced shutdown: %v", err)
	}
	log.Println("[main] shutdown complete")
}
