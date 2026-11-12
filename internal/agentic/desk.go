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
	SettingFacts         = "agent_facts"
)

// DeskSettings are the editor-controlled limits on the agent.
type DeskSettings struct {
	Enabled       bool
	Autopublish   bool
	MaxTurns      int
	StoriesPerRun int
	StoryBudget   int
	Facts         string // current facts the agent must trust over its own (outdated) memory, one per line
}

// Desk runs the agentic newsroom in stages (stages.go): an editor agent picks
// which incoming stories to cover; for each, a reporter agent verifies it, a
// writer drafts it, a reviewer checks the draft, and the reporter decides.
type Desk struct {
	worker   *cron.Worker
	agents   repository.AgentRepository
	journeys repository.JourneyRepository
	settings repository.SettingsRepository
	feeds    repository.FeedRepository
	search   SearchFunc
	read     ReadFunc
	chat     ChatFunc

	mu      sync.Mutex
	stageMu sync.Mutex // one stage at a time
	sess    *session
	running bool
	current string
	runID   uuid.UUID
}

func NewDesk(worker *cron.Worker, agents repository.AgentRepository, journeys repository.JourneyRepository,
	settings repository.SettingsRepository, feeds repository.FeedRepository, search SearchFunc, read ReadFunc) *Desk {
	return &Desk{worker: worker, chat: worker.AIChat, agents: agents, journeys: journeys, settings: settings, feeds: feeds, search: search, read: read}
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
		Facts:         d.setting(SettingFacts, ""),
	}
}

// SaveSettings stores new limits.
func (d *Desk) SaveSettings(s DeskSettings) error {
	for k, v := range map[string]string{
		SettingEnabled: strconv.FormatBool(s.Enabled), SettingAutopublish: strconv.FormatBool(s.Autopublish),
		SettingMaxTurns: strconv.Itoa(s.MaxTurns), SettingStoriesPerRun: strconv.Itoa(s.StoriesPerRun),
		SettingStoryBudget: strconv.Itoa(s.StoryBudget), SettingFacts: strings.TrimSpace(s.Facts),
	} {
		if err := d.settings.Set(k, v); err != nil {
			return err
		}
	}
	return nil
}

// addFact appends a verified current fact to the desk's memory (newest last,
// at most 40 lines, no duplicates).
func (d *Desk) addFact(line string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var lines []string
	for _, l := range strings.Split(d.setting(SettingFacts, ""), "\n") {
		if l = strings.TrimSpace(l); l != "" && l != line {
			lines = append(lines, l)
		}
	}
	lines = append(lines, line)
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return d.settings.Set(SettingFacts, strings.Join(lines, "\n"))
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
	sess, ok := d.begin(trigger)
	if !ok {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.runID, false
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()
		d.drive(ctx, sess)
	}()
	return sess.run.ID, true
}

// RunOnce runs the desk synchronously (for one-shot runners such as the
// news-worker command on GitHub Actions).
func (d *Desk) RunOnce(ctx context.Context, trigger string) (*domain.AgentRun, error) {
	sess, ok := d.begin(trigger)
	if !ok {
		return nil, ErrRunBusy
	}
	d.drive(ctx, sess)
	return sess.run, nil
}

func (d *Desk) finish(sess *session) {
	sess.watchdog.Stop()
	now := time.Now()
	sess.run.FinishedAt = &now
	if err := d.agents.SaveRun(sess.run); err != nil {
		log.Printf("[agent] could not save run: %v", err)
	}
	d.mu.Lock()
	d.running, d.current, d.sess = false, "", nil
	d.mu.Unlock()
}

func (d *Desk) setCurrent(s string) {
	d.mu.Lock()
	d.current = s
	d.mu.Unlock()
}

const (
	storyReserves = 2
	// Runs start every 15 minutes. No new story starts after runWorkWindow,
	// and the story in progress is stopped at runHardStop.
	runWorkWindow = 9 * time.Minute
	runHardStop   = 14 * time.Minute
)

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

// salvage handles an agent that hit a limit before deciding: a verified,
// written draft is held for an editor; otherwise the editor is asked.
func (d *Desk) salvage(ctx context.Context, s *story, out Outcome) string {
	if dr := s.latest; dr != nil && s.verified != nil && *s.verified {
		in := cron.ExternalArticle{
			NewsID: s.item.NewsID, SourceURL: s.item.SourceURL, RelatedURLs: s.item.RelatedURLs,
			Title: dr.Title, Body: dr.Body, Summary: dr.Summary, Tags: dr.Tags,
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
