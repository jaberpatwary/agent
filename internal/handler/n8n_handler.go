package handler

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/repository"
)

// N8NHandler exposes the machine-to-machine API an n8n workflow uses to
// take over the AI rewrite step (see internal/cron/external.go).
type N8NHandler struct {
	worker *cron.Worker
	apiKey string
}

func NewN8NHandler(worker *cron.Worker, apiKey string) *N8NHandler {
	return &N8NHandler{worker: worker, apiKey: apiKey}
}

// RequireAPIKey checks the X-API-Key header in constant time.
func (h *N8NHandler) RequireAPIKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := c.GetHeader("X-API-Key")
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(h.apiKey)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing X-API-Key"})
			return
		}
		c.Next()
	}
}

// Pending handles GET /api/n8n/pending?limit=N (default 5, max 25).
func (h *N8NHandler) Pending(c *gin.Context) {
	limit := 5
	if v, err := strconv.Atoi(c.Query("limit")); err == nil {
		limit = v
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 25 {
		limit = 25
	}

	items, err := h.worker.PendingItems(c.Request.Context(), limit)
	if errors.Is(err, cron.ErrPendingBusy) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		log.Printf("[n8n] pending items failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load pending items"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"count":                  len(items),
		"draft_json_placeholder": cron.DraftJSONPlaceholder,
		"items":                  items,
	})
}

// CreateArticle handles POST /api/n8n/articles.
func (h *N8NHandler) CreateArticle(c *gin.Context) {
	var in cron.ExternalArticle
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	article, err := h.worker.SaveExternal(c.Request.Context(), in)
	switch {
	case errors.Is(err, repository.ErrDuplicateArticle):
		c.JSON(http.StatusConflict, gin.H{"error": "article already exists", "source_url": in.SourceURL})
		return
	case errors.Is(err, cron.ErrInvalidExternal):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	case err != nil:
		log.Printf("[n8n] save failed for %q: %v", in.SourceURL, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save article"})
		return
	}

	log.Printf("[n8n] saved article %q (status=%s)", article.Title, article.Status)
	c.JSON(http.StatusCreated, gin.H{
		"id":            article.ID,
		"status":        article.Status,
		"review_reason": article.ReviewReason,
		"url":           "/article/" + article.ID.String(),
	})
}

// UpdateJourney handles PUT /api/n8n/journey — one stage update for one story.
func (h *N8NHandler) UpdateJourney(c *gin.Context) {
	var in cron.JourneyUpdate
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	j, err := h.worker.RecordJourney(in)
	switch {
	case errors.Is(err, cron.ErrInvalidStage):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	case errors.Is(err, cron.ErrJourneyNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case err != nil:
		log.Printf("[n8n] journey %s/%s failed: %v", in.NewsID, in.Stage, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update journey"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"news_id":           j.NewsID,
		"stage":             in.Stage,
		"published_status":  j.PublishedStatus,
		"fake_check_status": j.FakeCheckStatus,
		"cross_check_count": j.CrossCheckCount,
	})
}

// AI handles POST /api/n8n/ai — one AI step for the n8n workflow, answered by
// whichever provider is available (see ai.Gateway). The response mimics the
// OpenAI chat format so the workflow's parsing code works for any provider.
func (h *N8NHandler) AI(c *gin.Context) {
	var req ai.GatewayRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.User) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: need task, system, user"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 170*time.Second)
	defer cancel()
	res, attempts := h.worker.AIComplete(ctx, req)
	tried := make([]string, 0, len(attempts))
	for _, a := range attempts {
		status := "ok"
		if a.Err != nil {
			status = a.Err.Error()
			if len(status) > 160 {
				status = status[:160]
			}
		}
		tried = append(tried, a.Provider+" "+a.Model+": "+status)
	}
	if res.Err != nil {
		log.Printf("[n8n] ai %s failed: %v", req.Task, res.Err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": res.Err.Error(), "attempts": tried})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"provider": res.Provider,
		"model":    res.Model,
		"choices":  []gin.H{{"message": gin.H{"role": "assistant", "content": res.Text}}},
		"usage":    gin.H{"total_tokens": res.TokensUsed},
		"attempts": tried,
	})
}
