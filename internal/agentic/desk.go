package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

// Settings keys (editable on /admin/agent).
const (
	SettingEnabled       = "agent_enabled"
	SettingAutopublish   = "agent_autopublish"
	SettingMaxTurns      = "agent_max_turns"
	SettingStoriesPerRun = "agent_stories_per_run"
	SettingStoryBudget   = "agent_story_token_budget"
)

// DeskSettings are the editor-controlled limits on the agent.
type DeskSettings struct {
	Enabled       bool
	Autopublish   bool
	MaxTurns      int
	StoriesPerRun int
	StoryBudget   int
}

// Desk runs the agentic newsroom: an editor agent picks which incoming
// stories to cover, then a reporter agent works each one with tools until
// it publishes, holds, rejects, or asks a human.
type Desk struct {
	worker   *cron.Worker
	agents   repository.AgentRepository
	journeys repository.JourneyRepository
	settings repository.SettingsRepository
	feeds    repository.FeedRepository
	search   SearchFunc
	read     ReadFunc

	mu      sync.Mutex
	running bool
	current string
	runID   uuid.UUID
}

func NewDesk(worker *cron.Worker, agents repository.AgentRepository, journeys repository.JourneyRepository,
	settings repository.SettingsRepository, feeds repository.FeedRepository, search SearchFunc, read ReadFunc) *Desk {
	return &Desk{worker: worker, agents: agents, journeys: journeys, settings: settings, feeds: feeds, search: search, read: read}
}

func (d *Desk) setting(key, def string) string {
	v, ok, err := d.settings.Get(key)
	if err != nil || !ok || strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

func atoiOr(s string, def, lo, hi int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return min(max(n, lo), hi)
}

// Settings returns the current limits (with defaults).
func (d *Desk) Settings() DeskSettings {
	return DeskSettings{
		Enabled:       d.setting(SettingEnabled, "true") == "true",
		Autopublish:   d.setting(SettingAutopublish, "true") == "true",
		MaxTurns:      atoiOr(d.setting(SettingMaxTurns, "16"), 16, 4, 40),
		StoriesPerRun: atoiOr(d.setting(SettingStoriesPerRun, "1"), 1, 1, 5),
		StoryBudget:   atoiOr(d.setting(SettingStoryBudget, "250000"), 250000, 20000, 2000000),
	}
}

// SaveSettings stores new limits.
func (d *Desk) SaveSettings(s DeskSettings) error {
	for k, v := range map[string]string{
		SettingEnabled: strconv.FormatBool(s.Enabled), SettingAutopublish: strconv.FormatBool(s.Autopublish),
		SettingMaxTurns: strconv.Itoa(s.MaxTurns), SettingStoriesPerRun: strconv.Itoa(s.StoriesPerRun),
		SettingStoryBudget: strconv.Itoa(s.StoryBudget),
	} {
		if err := d.settings.Set(k, v); err != nil {
			return err
		}
	}
	return nil
}

// DeskStatus is what /api/agent/status and /admin/agent show.
type DeskStatus struct {
	Running bool              `json:"running"`
	RunID   string            `json:"run_id,omitempty"`
	Current string            `json:"current,omitempty"`
	Recent  []domain.AgentRun `json:"recent"`
}

func (d *Desk) Status() DeskStatus {
	d.mu.Lock()
	st := DeskStatus{Running: d.running, Current: d.current}
	if d.running {
		st.RunID = d.runID.String()
	}
	d.mu.Unlock()
	st.Recent, _ = d.agents.RecentRuns(10)
	return st
}

// Start launches a run in the background. It returns false if one is already running.
func (d *Desk) Start(trigger string) (uuid.UUID, bool) {
	d.mu.Lock()
	if d.running {
		id := d.runID
		d.mu.Unlock()
		return id, false
	}
	run := &domain.AgentRun{ID: uuid.New(), Trigger: trigger, Status: "running", StartedAt: time.Now()}
	d.running, d.runID, d.current = true, run.ID, "খবর খোঁজা হচ্ছে"
	d.mu.Unlock()
	if err := d.agents.CreateRun(run); err != nil {
		log.Printf("[agent] could not record run: %v", err)
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[agent] run panicked: %v", r)
				run.Status, run.Error = "failed", fmt.Sprint(r)
				d.finish(run)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()
		d.run(ctx, run)
	}()
	return run.ID, true
}

// RunOnce runs the desk synchronously (for one-shot runners such as the
// news-worker command on GitHub Actions).
func (d *Desk) RunOnce(ctx context.Context, trigger string) (*domain.AgentRun, error) {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return nil, fmt.Errorf("a run is already in progress")
	}
	run := &domain.AgentRun{ID: uuid.New(), Trigger: trigger, Status: "running", StartedAt: time.Now()}
	d.running, d.runID = true, run.ID
	d.mu.Unlock()
	if err := d.agents.CreateRun(run); err != nil {
		log.Printf("[agent] could not record run: %v", err)
	}
	d.run(ctx, run)
	return run, nil
}

func (d *Desk) finish(run *domain.AgentRun) {
	now := time.Now()
	run.FinishedAt = &now
	if err := d.agents.SaveRun(run); err != nil {
		log.Printf("[agent] could not save run: %v", err)
	}
	d.mu.Lock()
	d.running, d.current = false, ""
	d.mu.Unlock()
}

func (d *Desk) setCurrent(s string) {
	d.mu.Lock()
	d.current = s
	d.mu.Unlock()
}

func (d *Desk) run(ctx context.Context, run *domain.AgentRun) {
	defer d.finish(run)
	cfg := d.Settings()
	if !cfg.Enabled {
		run.Status, run.Error = "skipped", "agent is switched off on /admin/agent"
		return
	}
	if budget := d.worker.DailyTokenBudget(); budget > 0 && d.worker.TokensUsedToday() >= budget {
		run.Status, run.Error = "skipped", "daily AI token budget reached"
		return
	}
	items, err := d.worker.PendingItems(ctx, 12)
	if err != nil {
		run.Status, run.Error = "failed", "could not load new stories: "+err.Error()
		return
	}
	run.Candidates = len(items)
	if len(items) == 0 {
		run.Status, run.PlanNote = "done", "কোনো নতুন খবর নেই"
		return
	}

	d.setCurrent("সম্পাদক এজেন্ট খবর বাছাই করছে")
	chosen, note := d.plan(ctx, run, items, cfg.StoriesPerRun)
	run.PlanNote = note
	_ = d.agents.SaveRun(run)

	for _, item := range chosen {
		if ctx.Err() != nil {
			break
		}
		d.setCurrent(item.Title)
		tokens := d.processStory(ctx, run, item, cfg)
		run.Processed++
		run.Tokens += tokens
		_ = d.agents.SaveRun(run)
	}
	run.Status = "done"
}

// plan is the editor agent: from the candidates it picks what to cover now.
func (d *Desk) plan(ctx context.Context, run *domain.AgentRun, items []cron.PendingItem, k int) ([]cron.PendingItem, string) {
	if len(items) <= k {
		return items, "সব নতুন খবর নেওয়া হলো"
	}
	var b strings.Builder
	for i, it := range items {
		fmt.Fprintf(&b, "[%d] %s | %s | %s | %d chars | %d other outlets | %s\n", i, it.Category, it.SourceName,
			it.PublishedAt.In(dhaka()).Format("01-02 15:04"), len([]rune(plainText(it.Content))), len(it.RelatedURLs), it.Title)
	}
	system := `You are the duty editor of a Bangla news portal. From the incoming stories pick which to cover first.
Prefer: important public-interest news, fresh events, stories other outlets also carry, a balance of sections. Deprioritise: trivia, promotional or routine notices, opinion pieces, near-duplicates of another candidate.
Reply ONLY with JSON: {"cover":[{"index":0,"why":"…"}], "skip":[{"index":3,"why":"…"}], "note":"one sentence in Bangla about today's priorities"}. List "cover" in priority order.`
	res, err := d.worker.AIText(ctx, ai.GatewayRequest{Task: "plan", JSON: true, MaxTokens: 1200, System: system, User: "Incoming stories:\n" + b.String()})
	var picked []cron.PendingItem
	note := ""
	if err == nil {
		var p struct {
			Cover []struct {
				Index int    `json:"index"`
				Why   string `json:"why"`
			} `json:"cover"`
			Note string `json:"note"`
		}
		if json.Unmarshal([]byte(extractJSON(res.Text)), &p) == nil {
			seen := map[int]bool{}
			var why []string
			for _, c := range p.Cover {
				if c.Index >= 0 && c.Index < len(items) && !seen[c.Index] && len(picked) < k {
					seen[c.Index] = true
					picked = append(picked, items[c.Index])
					why = append(why, items[c.Index].Title+" — "+c.Why)
				}
			}
			note = strings.TrimSpace(p.Note + " | বাছাই: " + strings.Join(why, "; "))
		}
		_ = d.agents.AddStep(&domain.AgentStep{RunID: run.ID, Kind: "plan", Tool: "editor_agent", Input: clip(b.String(), 4000),
			Output: clip(res.Text, 4000), Provider: res.Provider, Model: res.Model, Tokens: res.TokensUsed})
	}
	if len(picked) == 0 {
		sort.SliceStable(items, func(i, j int) bool { return items[i].PublishedAt.After(items[j].PublishedAt) })
		picked = items[:k]
		note = "সম্পাদক এজেন্ট পাওয়া যায়নি — সবচেয়ে নতুন খবর নেওয়া হলো"
	}
	return picked, note
}

func (d *Desk) outletNames() []string {
	feeds, err := d.feeds.List()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(feeds))
	for _, f := range feeds {
		names = append(names, f.Name)
	}
	return names
}

// processStory lets the reporter agent work one story; returns tokens used.
func (d *Desk) processStory(ctx context.Context, run *domain.AgentRun, item cron.PendingItem, cfg DeskSettings) int {
	if _, err := d.worker.RecordJourney(cron.JourneyUpdate{NewsID: item.NewsID, Stage: "received", SourceURL: item.SourceURL,
		SourceName: item.SourceName, Category: item.Category, OriginalTitle: item.Title, OriginalContent: item.Content}); err != nil {
		log.Printf("[agent] journey %s: %v", item.NewsID, err)
	}
	j, _ := d.journeys.Get(item.NewsID)
	mem := memory{Attempt: 1}
	if j != nil {
		mem.Attempt = j.Attempts + 1
		mem.PrevSummary, mem.EditorAnswer, mem.AgentQ = j.AgentSummary, j.EditorAnswer, j.AgentQuestion
	}
	mem.Feedback, _ = d.agents.RecentFeedback(8)
	mem.Source, _ = d.agents.SourceRecord(item.SourceName)

	houseStyle, sensitive := d.worker.RunSettings()
	setJourney := func(fields map[string]any) {
		if err := d.journeys.Update(item.NewsID, fields); err != nil {
			log.Printf("[agent] journey update %s: %v", item.NewsID, err)
		}
	}
	setJourney(map[string]any{"attempts": mem.Attempt})

	s := &story{item: item, nr: d.worker, versions: d.agents, searchNews: d.search, readPage: d.read,
		setJourney: setJourney, outlets: d.outletNames(), autopublish: cfg.Autopublish}

	step := 0
	engine := Engine{
		Chat:        d.worker.AIChat,
		Tools:       s.tools(),
		MaxTurns:    cfg.MaxTurns,
		TokenBudget: cfg.StoryBudget,
		MaxTokens:   6000,
		Record: func(r StepRecord) {
			step++
			if err := d.agents.AddStep(&domain.AgentStep{RunID: run.ID, NewsID: item.NewsID, Step: step, Kind: r.Kind, Tool: r.Tool,
				Input: r.Input, Output: clip(r.Output, 6000), Provider: r.Provider, Model: r.Model, Tokens: r.Tokens}); err != nil {
				log.Printf("[agent] step %s/%d: %v", item.NewsID, step, err)
			}
		},
	}
	out := engine.Run(ctx, systemPrompt(houseStyle, sensitive[item.Category], cfg.Autopublish, mem), goalMessage(item, mem))

	summary := ""
	switch {
	case out.Finished:
		summary = out.Tool + ": " + out.Result
	case out.Stopped == "provider_error":
		summary = "AI প্রোভাইডার পাওয়া যায়নি — পরের রানে আবার চেষ্টা হবে (" + clip(out.Detail, 160) + ")"
	default:
		summary = d.salvage(ctx, s, out)
	}
	fields := map[string]any{"agent_summary": clip(summary, 1000), "agent_steps": step, "agent_tokens": out.Tokens}
	if mem.EditorAnswer != "" && out.Finished {
		fields["editor_answer"] = "" // answer consumed
	}
	setJourney(fields)
	log.Printf("[agent] %q → %s (%d turns, %d tokens)", clip(item.Title, 60), clip(summary, 120), out.Turns, out.Tokens)
	return out.Tokens
}

// salvage handles an agent that hit a limit before deciding: a verified,
// written draft is held for an editor; otherwise the editor is asked.
func (d *Desk) salvage(ctx context.Context, s *story, out Outcome) string {
	if s.draft != nil && s.verified != nil && *s.verified {
		in := cron.ExternalArticle{
			NewsID: s.item.NewsID, SourceURL: s.item.SourceURL, RelatedURLs: s.item.RelatedURLs,
			Title: s.draft.Title, Body: s.draft.Body, Summary: s.draft.Summary, Tags: s.draft.Tags,
			ImageURL: s.item.ImageURL, SourceName: s.item.SourceName, Category: s.item.Category, PublishedAt: s.item.PublishedAt,
			OriginalTitle: s.item.Title, OriginalContent: s.item.Content,
			ForceReview: "এজেন্ট সীমার মধ্যে শেষ করতে পারেনি (" + out.Stopped + ") — খসড়াটি দেখে সিদ্ধান্ত দিন",
		}
		if art, err := d.worker.SaveExternal(ctx, in); err == nil {
			return "limit reached (" + out.Stopped + "); draft held for editor, article_id=" + art.ID.String()
		}
	}
	q := "এজেন্ট নির্ধারিত সীমার মধ্যে কাজ শেষ করতে পারেনি (" + out.Stopped + ": " + out.Detail + ")। আবার চেষ্টা করবে, নাকি বাদ দেবে?"
	s.setJourney(map[string]any{"published_status": "needs_editor", "agent_question": q})
	return "stopped: " + out.Stopped + " — asked editor"
}

// RecordEditorDecision stores a human decision so the agent can learn from it.
func (d *Desk) RecordEditorDecision(newsID string, articleID *uuid.UUID, action, reason, title, source string) {
	if err := d.agents.AddFeedback(&domain.EditorFeedback{NewsID: newsID, ArticleID: articleID, Action: action,
		Reason: strings.TrimSpace(reason), Title: title, SourceName: source}); err != nil {
		log.Printf("[agent] feedback: %v", err)
	}
}

// AnswerQuestion stores an editor's reply and puts the story back in the queue.
func (d *Desk) AnswerQuestion(newsID, answer string) error {
	j, err := d.journeys.Get(newsID)
	if err != nil {
		return err
	}
	if err := d.journeys.Update(newsID, map[string]any{"editor_answer": strings.TrimSpace(answer), "published_status": "pending"}); err != nil {
		return err
	}
	d.RecordEditorDecision(newsID, nil, "answered", answer, j.OriginalTitle, j.SourceName)
	return nil
}

// Steps returns a story's full agent trace.
func (d *Desk) Steps(newsID string) ([]domain.AgentStep, error) { return d.agents.StepsForNews(newsID) }
