package cron

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"bangla-news-aggregator/internal/agent"
	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

// Helpers the agentic desk (internal/agentic) uses on top of the worker's
// feed fetching, dedupe, image caching and saving.

// RunSettings returns the admin-editable house style and the categories that
// must never be published without an editor.
func (w *Worker) RunSettings() (houseStyle string, sensitive map[string]bool) {
	return w.loadRunSettings()
}

// TokensUsedToday is today's (UTC) AI token total across all providers.
func (w *Worker) TokensUsedToday() int {
	used, err := w.usageRepo.TokensUsedSince(time.Now().UTC().Truncate(24 * time.Hour))
	if err != nil {
		return 0
	}
	return used
}

// DailyTokenBudget is AI_DAILY_TOKEN_BUDGET (0 = unlimited).
func (w *Worker) DailyTokenBudget() int { return w.cfg.AIDailyTokenBudget }

// LogAIAttempts records provider calls on /admin/ai-usage under a prefix (e.g. "agent").
func (w *Worker) LogAIAttempts(prefix string, attempts []ai.Result) {
	for _, a := range attempts {
		if a.Provider == "" {
			continue
		}
		a.Provider = prefix + ":" + a.Provider
		w.logAIUsage(a)
	}
}

// AIChat runs one agent turn (with tools) through the provider gateway.
func (w *Worker) AIChat(ctx context.Context, system string, msgs []ai.ChatMessage, tools []ai.ToolSpec, maxTokens int) (ai.ChatResult, error) {
	res, attempts := w.gateway.Chat(ctx, system, msgs, tools, maxTokens)
	w.LogAIAttempts("agent", attempts)
	if res.Provider == "" {
		last := errors.New("no AI provider answered")
		if n := len(attempts); n > 0 && attempts[n-1].Err != nil {
			last = attempts[n-1].Err
		}
		return res, last
	}
	return res, nil
}

// AIText runs a single prompt (optionally with web search) through the gateway.
func (w *Worker) AIText(ctx context.Context, req ai.GatewayRequest) (ai.Result, error) {
	res, attempts := w.gateway.Run(ctx, req)
	w.LogAIAttempts("agent", attempts)
	return res, res.Err
}

// SearchArticles looks for stories the portal already has (any status).
func (w *Worker) SearchArticles(query string, limit int) ([]domain.Article, error) {
	list, _, err := w.articleRepo.Search(limit, 0, query, "", "")
	return list, err
}

// GetArticle loads one article.
func (w *Worker) GetArticle(id uuid.UUID) (*domain.Article, error) { return w.articleRepo.GetByID(id) }

// UpdateExternal rewrites an existing article with a newer, fuller version of
// the same developing story. The previous text is kept as a version.
func (w *Worker) UpdateExternal(ctx context.Context, articleID uuid.UUID, in ExternalArticle, note string, versions repository.AgentRepository) (*domain.Article, error) {
	existing, err := w.articleRepo.GetByID(articleID)
	if err != nil || existing == nil {
		return nil, fmt.Errorf("%w: article %s not found", ErrInvalidExternal, articleID)
	}
	if n := visibleLen(in.Body); n < 200 {
		return nil, fmt.Errorf("%w: body too short (%d visible chars)", ErrInvalidExternal, n)
	}
	v, err := versions.NextVersion(articleID)
	if err != nil {
		return nil, err
	}
	if err := versions.SaveVersion(&domain.ArticleVersion{
		ArticleID: articleID, Version: v, Title: existing.Title, Content: existing.Content, Summary: existing.Summary, Note: note,
	}); err != nil {
		return nil, err
	}
	summary := strings.TrimSpace(in.Summary)
	if summary == "" {
		summary = truncate(strings.Join(strings.Fields(htmlTagRegex.ReplaceAllString(in.Body, " ")), " "), 280)
	}
	if err := w.articleRepo.UpdateStory(articleID, strings.TrimSpace(in.Title), strings.TrimSpace(in.Body), summary,
		strings.Join(cleanExternalTags(in.Tags), ", ")); err != nil {
		return nil, err
	}
	if err := w.seenURLRepo.RecordAll(articleID, append([]string{in.SourceURL}, in.RelatedURLs...)); err != nil {
		log.Printf("[agent] failed to record seen URLs for update %s: %v", articleID, err)
	}
	now := time.Now()
	w.finishJourney(in.NewsID, map[string]any{"published_status": "updated", "published_at": &now, "article_id": &articleID})
	return w.articleRepo.GetByID(articleID)
}

// FactCheckNumbers exposes the number-consistency check to the agent's review step.
func FactCheckNumbers(original []string, body string) agent.FactCheckResult {
	return agent.CheckFactConsistency(original, body)
}
