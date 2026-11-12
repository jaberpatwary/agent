package agentic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/domain"
)

// A run is a chain of stages, each the work of one agent. The driver — the
// n8n workflow, where every stage is a node, or drive() inside the app —
// calls a stage and goes where its result's Next says.
const (
	StagePlan   = "plan"   // editor agent: which stories to cover
	StageVerify = "verify" // reporter agent: is the next story real and new
	StageWrite  = "write"  // writer: draft it, or fix the last draft
	StageReview = "review" // reviewer: hard rules + AI editor
	StageDecide = "decide" // reporter agent: publish, hold, reject or ask
	StageFinish = "finish" // close the run
)

var (
	ErrRunBusy   = errors.New("a run is already in progress")
	ErrNoSuchRun = errors.New("no such run in progress")
)

// StageResult is what a stage tells its driver.
type StageResult struct {
	RunID  string `json:"run_id"`
	Stage  string `json:"stage"`
	Agent  string `json:"agent"`
	Next   string `json:"next"` // the stage to call now
	Story  string `json:"story,omitempty"`
	NewsID string `json:"news_id,omitempty"`
	Result string `json:"result"`
	Detail any    `json:"detail,omitempty"`
	// The whole stories, so every node of the workflow shows the full news.
	Source *StoryText `json:"source,omitempty"` // the original, as its outlet published it
	Draft  *StoryText `json:"draft,omitempty"`  // our article
}

// StoryText is a whole story as readable text.
type StoryText struct {
	Title   string   `json:"title"`
	Outlet  string   `json:"outlet,omitempty"`
	URL     string   `json:"url,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Chars   int      `json:"chars"`
	Text    string   `json:"text"` // every paragraph, separated by blank lines
}

func storyText(title, htmlBody string) *StoryText {
	var paras []string
	for _, p := range paraSplit.Split(htmlBody, -1) {
		if t := plainText(p); t != "" {
			paras = append(paras, t)
		}
	}
	text := strings.Join(paras, "\n\n")
	return &StoryText{Title: title, Text: text, Chars: utf8.RuneCountInString(plainText(htmlBody))}
}

func draftText(d *draft) *StoryText {
	t := storyText(d.Title, d.Body)
	t.Summary, t.Tags = d.Summary, d.Tags
	return t
}

// session is a run in progress.
type session struct {
	run       *domain.AgentRun
	cfg       DeskSettings
	queue     []cron.PendingItem // chosen by the editor agent, not yet started
	published int
	down      bool // no AI capacity right now: the next run tries again
	job       *job
	watchdog  *time.Timer
}

// job is the story being worked on, carried from stage to stage.
type job struct {
	s       *story
	mem     memory
	system  string
	history []ai.ChatMessage // the reporter's conversation, continued in the decide stage
	steps   int
	tokens  int
	record  func(StepRecord)
}

const (
	// The writer gets failed drafts back until this long into the run, and
	// one review is kept for the reporter's own correction.
	runFixUntil     = 10 * time.Minute
	decideTurns     = 4
	timeUpSummary   = "এই রানের সময় শেষ — পরের রানে আবার চেষ্টা হবে"
	writerReviewCap = maxReviews - 1
)

var errNoSourceText = errors.New("the source has no text to rewrite")

func (d *Desk) session() *session {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sess
}

// begin opens a run, unless one is in progress.
func (d *Desk) begin(trigger string) (*session, bool) {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return nil, false
	}
	run := &domain.AgentRun{ID: uuid.New(), Trigger: trigger, Status: "running", StartedAt: time.Now()}
	sess := &session{run: run}
	d.running, d.runID, d.current, d.sess = true, run.ID, "খবর খোঁজা হচ্ছে", sess
	// A driver that dies between stages must not block every later run.
	sess.watchdog = time.AfterFunc(runHardStop+time.Minute, func() { d.abandon(sess) })
	d.mu.Unlock()
	if err := d.agents.CreateRun(run); err != nil {
		log.Printf("[agent] could not record run: %v", err)
	}
	return sess, true
}

func (d *Desk) abandon(sess *session) {
	d.stageMu.Lock()
	defer d.stageMu.Unlock()
	if d.session() != sess {
		return
	}
	log.Printf("[agent] run %s was not finished by its driver; closing it", sess.run.ID)
	if sess.run.Status == "running" {
		sess.run.Status, sess.run.Error = "interrupted", "ধাপগুলো সময়মতো শেষ হয়নি — রানটি বন্ধ করা হলো"
	}
	d.stageFinish(sess)
}

// Stage runs one stage for an outside driver. The plan stage opens a run;
// the others continue the run with that id.
func (d *Desk) Stage(ctx context.Context, name, runID, trigger string) (StageResult, error) {
	switch name {
	case StagePlan:
		sess, ok := d.begin(trigger)
		if !ok {
			return StageResult{Stage: name, Next: StageFinish, Result: "আগের রান এখনো চলছে"}, ErrRunBusy
		}
		return d.stage(ctx, sess, name), nil
	case StageVerify, StageWrite, StageReview, StageDecide, StageFinish:
		sess := d.session()
		if sess == nil || sess.run.ID.String() != runID {
			return StageResult{RunID: runID, Stage: name, Next: StageFinish, Result: "এই রানটি চালু নেই"}, ErrNoSuchRun
		}
		return d.stage(ctx, sess, name), nil
	}
	return StageResult{}, fmt.Errorf("unknown stage %q", name)
}

// drive runs every stage in turn, for a run started inside the app.
func (d *Desk) drive(ctx context.Context, sess *session) {
	next := StagePlan
	for {
		res := d.stage(ctx, sess, next)
		if next == StageFinish {
			return
		}
		if next = res.Next; ctx.Err() != nil {
			next = StageFinish
		}
	}
}

func (d *Desk) stage(ctx context.Context, sess *session, name string) (res StageResult) {
	d.stageMu.Lock()
	defer d.stageMu.Unlock()
	res = StageResult{RunID: sess.run.ID.String(), Stage: name, Next: StageFinish}
	if d.session() != sess {
		res.Result = "রানটি আগেই শেষ হয়েছে"
		return res
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[agent] stage %s panicked: %v", name, r)
			sess.run.Status, sess.run.Error = "failed", fmt.Sprint(r)
			res = d.stageFinish(sess)
		}
		res.RunID, res.Stage = sess.run.ID.String(), name
	}()
	// A story may not run into the next scheduled run, which would then be skipped.
	ctx, cancel := context.WithDeadline(ctx, sess.run.StartedAt.Add(runHardStop))
	defer cancel()
	switch name {
	case StagePlan:
		return d.stagePlan(ctx, sess)
	case StageVerify:
		return d.stageVerify(ctx, sess)
	case StageWrite:
		return d.stageWrite(ctx, sess)
	case StageReview:
		return d.stageReview(ctx, sess)
	case StageDecide:
		return d.stageDecide(ctx, sess)
	}
	return d.stageFinish(sess)
}

// stagePlan is the editor agent: it loads the new stories and picks which to cover.
func (d *Desk) stagePlan(ctx context.Context, sess *session) StageResult {
	run := sess.run
	res := StageResult{Agent: "সম্পাদক এজেন্ট", Next: StageFinish}
	sess.cfg = d.Settings()
	if !sess.cfg.Enabled {
		run.Status, run.Error = "skipped", "agent is switched off on /admin/agent"
		res.Result = "এজেন্ট বন্ধ আছে"
		return res
	}
	if budget := d.worker.DailyTokenBudget(); budget > 0 && d.worker.TokensUsedToday() >= budget {
		run.Status, run.Error = "skipped", "daily AI token budget reached"
		res.Result = "আজকের AI টোকেন বাজেট শেষ"
		return res
	}
	items, err := d.worker.PendingItems(ctx, 12)
	if err != nil {
		run.Status, run.Error = "failed", "could not load new stories: "+err.Error()
		res.Result = "নতুন খবর আনা যায়নি"
		return res
	}
	run.Candidates = len(items)
	if len(items) == 0 {
		run.PlanNote = "কোনো নতুন খবর নেই"
		res.Result = run.PlanNote
		return res
	}

	d.setCurrent("সম্পাদক এজেন্ট খবর বাছাই করছে")
	// A couple of reserves: when a story is rejected or held, the desk moves
	// on to the next one so every run tries to put new news on the portal.
	sess.queue, run.PlanNote = d.plan(ctx, run, items, sess.cfg.StoriesPerRun+storyReserves)
	_ = d.agents.SaveRun(run)

	chosen := make([]map[string]any, 0, len(sess.queue))
	for _, it := range sess.queue {
		chosen = append(chosen, map[string]any{"title": it.Title, "outlet": it.SourceName, "url": it.SourceURL,
			"chars": utf8.RuneCountInString(plainText(it.Content))})
	}
	res.Next, res.Result = StageVerify, run.PlanNote
	res.Detail = map[string]any{"candidates": len(items), "chosen": chosen}
	return res
}

// nextStory says where a run goes once a story is closed.
func (sess *session) nextStory() string {
	if sess.down || len(sess.queue) == 0 || sess.published >= sess.cfg.StoriesPerRun ||
		time.Since(sess.run.StartedAt) > runWorkWindow { // the next scheduled run is close; leave the rest to it
		return StageFinish
	}
	return StageVerify
}

// openStory starts the next chosen story and gathers what the desk knows about it.
func (d *Desk) openStory(sess *session) *job {
	item := sess.queue[0]
	sess.queue = sess.queue[1:]
	if _, err := d.worker.RecordJourney(cron.JourneyUpdate{NewsID: item.NewsID, Stage: "received", SourceURL: item.SourceURL,
		SourceName: item.SourceName, Category: item.Category, OriginalTitle: item.Title, OriginalContent: item.Content}); err != nil {
		log.Printf("[agent] journey %s: %v", item.NewsID, err)
	}
	mem := memory{Attempt: 1, Facts: sess.cfg.Facts}
	if j, _ := d.journeys.Get(item.NewsID); j != nil {
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

	j := &job{mem: mem, system: systemPrompt(houseStyle, sensitive[item.Category], sess.cfg.Autopublish, mem)}
	j.s = &story{item: item, nr: d.worker, versions: d.agents, searchNews: d.search, readPage: d.read,
		setJourney: setJourney, outlets: d.outletNames(), autopublish: sess.cfg.Autopublish, addFact: d.addFact, facts: sess.cfg.Facts}
	j.record = func(r StepRecord) {
		j.steps++
		if err := d.agents.AddStep(&domain.AgentStep{RunID: sess.run.ID, NewsID: item.NewsID, Step: j.steps, Kind: r.Kind, Tool: r.Tool,
			Input: r.Input, Output: clip(r.Output, 6000), Provider: r.Provider, Model: r.Model, Tokens: r.Tokens}); err != nil {
			log.Printf("[agent] step %s/%d: %v", item.NewsID, j.steps, err)
		}
	}
	sess.job = j
	return j
}

// reporter lets the reporter agent work towards a goal with the given tools,
// continuing its earlier conversation on this story.
func (d *Desk) reporter(ctx context.Context, sess *session, j *job, tools []Tool, turns int, goal string) Outcome {
	e := Engine{Chat: d.chat, Tools: tools, MaxTurns: turns, MaxTokens: 6000, Record: j.record, History: j.history,
		TokenBudget: max(1, sess.cfg.StoryBudget-j.tokens)}
	out := e.Run(ctx, j.system, goal)
	j.history, j.tokens = e.History, j.tokens+out.Tokens
	if out.Stopped == "provider_error" {
		sess.down = true
	}
	return out
}

// outcomeSummary turns how the reporter stopped into the story's summary; an
// agent that hit a limit before deciding has its draft held for an editor.
func (d *Desk) outcomeSummary(ctx context.Context, j *job, out Outcome) string {
	switch {
	case out.Finished:
		return out.Tool + ": " + out.Result
	case out.Stopped == "provider_error" && errors.Is(ctx.Err(), context.DeadlineExceeded):
		return timeUpSummary
	case out.Stopped == "provider_error":
		return "AI প্রোভাইডার পাওয়া যায়নি — পরের রানে আবার চেষ্টা হবে (" + clip(out.Detail, 160) + ")"
	}
	return d.salvage(ctx, j.s, out)
}

// closeStory records the end of the work on the current story.
func (d *Desk) closeStory(sess *session, summary string, finished bool) {
	j := sess.job
	fields := map[string]any{"agent_summary": clip(summary, 1000), "agent_steps": j.steps, "agent_tokens": j.tokens}
	if j.mem.EditorAnswer != "" && finished {
		fields["editor_answer"] = "" // answer consumed
	}
	j.s.setJourney(fields)
	log.Printf("[agent] %q → %s (%d steps, %d tokens)", clip(j.s.item.Title, 60), clip(summary, 120), j.steps, j.tokens)
	sess.run.Processed++
	sess.run.Tokens += j.tokens
	_ = d.agents.SaveRun(sess.run)
	if j.s.saved != nil && j.s.saved.Status == "published" {
		sess.published++
	}
	sess.job = nil
}

func (j *job) result(res StageResult) StageResult {
	it := j.s.item
	res.Story, res.NewsID = it.Title, it.NewsID
	res.Source = storyText(it.Title, it.Content)
	res.Source.Outlet, res.Source.URL = it.SourceName, it.SourceURL
	return res
}

// stageVerify is the reporter agent's first stage: it settles whether the
// next story is real and new, choosing its own searches.
func (d *Desk) stageVerify(ctx context.Context, sess *session) StageResult {
	res := StageResult{Agent: "প্রতিবেদক এজেন্ট"}
	if sess.job != nil {
		d.closeStory(sess, timeUpSummary, false)
	}
	if res.Next = sess.nextStory(); res.Next == StageFinish {
		res.Result = "এই রানে আর খবর নেওয়া হবে না"
		return res
	}
	j := d.openStory(sess)
	s := j.s
	d.setCurrent("প্রতিবেদক এজেন্ট যাচাই করছে: " + s.item.Title)
	gathered := s.observe(ctx, j.record)

	out := d.reporter(ctx, sess, j, verifyTools(s), max(3, sess.cfg.MaxTurns-decideTurns), goalMessage(s.item, j.mem)+gathered)
	res.Detail = map[string]any{"corroborating_outlets": s.corroboration(), "searches": s.searches}
	if out.Finished && out.Tool == "record_verification" {
		res.Next = StageWrite
		res.Result = fmt.Sprintf("যাচাই হয়েছে — আরও %d টি সংবাদমাধ্যমে একই খবর", s.corroboration())
		return j.result(res)
	}
	res.Result = d.outcomeSummary(ctx, j, out)
	d.closeStory(sess, res.Result, out.Finished)
	res.Next = sess.nextStory()
	return j.result(res)
}

// stageWrite is the writer: a first draft, or the last one fixed after a failed review.
func (d *Desk) stageWrite(ctx context.Context, sess *session) StageResult {
	res := StageResult{Agent: "লেখক"}
	j := sess.job
	if j == nil {
		res.Next, res.Result = sess.nextStory(), "কোনো খবর খোলা নেই"
		return res
	}
	s := j.s
	d.setCurrent("লেখক লিখছেন: " + s.item.Title)
	notes := s.fixNotes()
	j.record(StepRecord{Kind: "tool", Tool: "write_draft", Input: notes})
	head, err := s.writeDraft(ctx, notes)
	if err != nil {
		j.record(StepRecord{Kind: "result", Tool: "write_draft", Output: "ERROR: " + err.Error()})
		switch {
		case errors.Is(err, errNoSourceText):
			res.Result = "মূল খবরে লেখার মতো টেক্সট নেই — বাদ"
			s.setJourney(map[string]any{"published_status": "rejected"})
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			res.Result, sess.down = timeUpSummary, true
		default:
			res.Result, sess.down = "লেখক পাওয়া যায়নি — পরের রানে আবার চেষ্টা হবে ("+shortErr(err)+")", true
		}
		d.closeStory(sess, res.Result, false)
		res.Next = sess.nextStory()
		return j.result(res)
	}
	j.record(StepRecord{Kind: "result", Tool: "write_draft", Output: head})
	res.Next, res.Result = StageReview, strings.SplitN(head, "\n", 2)[0]
	res.Draft = draftText(s.written)
	return j.result(res)
}

// stageReview is the reviewer: the hard rules in code and the AI editor. A
// failed draft goes back to the writer while reviews and time remain.
func (d *Desk) stageReview(ctx context.Context, sess *session) StageResult {
	res := StageResult{Agent: "পরীক্ষক", Next: StageDecide}
	j := sess.job
	if j == nil || j.s.written == nil {
		res.Next, res.Result = sess.nextStory(), "পরীক্ষা করার মতো খসড়া নেই"
		return res
	}
	s := j.s
	d.setCurrent("পরীক্ষক খসড়া দেখছেন: " + s.item.Title)
	j.record(StepRecord{Kind: "tool", Tool: "review_draft", Input: s.written.Title})
	v, err := s.runReview(ctx, s.written, "", nil)
	if err != nil {
		j.record(StepRecord{Kind: "result", Tool: "review_draft", Output: "ERROR: " + err.Error()})
		res.Result = err.Error()
		return j.result(res)
	}
	out, _ := json.MarshalIndent(v, "", " ")
	j.record(StepRecord{Kind: "result", Tool: "review_draft", Output: string(out)})
	res.Detail, res.Draft = v, draftText(s.written)
	switch {
	case v.Passed:
		res.Result = "পাস — খসড়া প্রকাশের যোগ্য"
	case s.reviews < writerReviewCap && time.Since(sess.run.StartedAt) < runFixUntil && ctx.Err() == nil:
		res.Next = StageWrite
		res.Result = fmt.Sprintf("ফেল — সমস্যা ঠিক করতে লেখকের কাছে ফেরত (%d/%d পরীক্ষা)", s.reviews, maxReviews)
	default:
		res.Result = fmt.Sprintf("ফেল — সিদ্ধান্তের জন্য প্রতিবেদকের কাছে (%d/%d পরীক্ষা)", s.reviews, maxReviews)
	}
	return j.result(res)
}

// stageDecide is the reporter agent's last stage: with the draft and its
// review in hand it publishes, holds, rejects or asks an editor.
func (d *Desk) stageDecide(ctx context.Context, sess *session) StageResult {
	res := StageResult{Agent: "প্রতিবেদক এজেন্ট"}
	j := sess.job
	if j == nil {
		res.Next, res.Result = sess.nextStory(), "কোনো খবর খোলা নেই"
		return res
	}
	s := j.s
	d.setCurrent("প্রতিবেদক এজেন্ট সিদ্ধান্ত নিচ্ছে: " + s.item.Title)
	out := d.reporter(ctx, sess, j, pick(s.tools(), "review_draft", "submit_article", "reject_story", "ask_editor"), decideTurns, decideGoal(s))
	res.Result = d.outcomeSummary(ctx, j, out)
	if s.saved != nil {
		a := s.saved
		res.Detail = map[string]any{"article_id": a.ID, "status": a.Status, "review_reason": a.ReviewReason}
		res.Draft = storyText(a.Title, a.Content)
		res.Draft.Summary, res.Draft.URL = a.Summary, "/article/"+a.ID.String()
	}
	d.closeStory(sess, res.Result, out.Finished)
	res.Next = sess.nextStory()
	return j.result(res)
}

// stageFinish closes the run.
func (d *Desk) stageFinish(sess *session) StageResult {
	if sess.job != nil {
		d.closeStory(sess, timeUpSummary, false)
	}
	run := sess.run
	if run.Status == "running" {
		run.Status = "done"
	}
	d.finish(sess)
	return StageResult{RunID: run.ID.String(), Stage: StageFinish, Agent: "ডেস্ক", Next: StageFinish,
		Result: fmt.Sprintf("রান শেষ: %d টি খবরে কাজ, %d টি প্রকাশিত", run.Processed, sess.published),
		Detail: map[string]any{"status": run.Status, "candidates": run.Candidates, "processed": run.Processed,
			"published": sess.published, "tokens": run.Tokens, "editor_agent_note": run.PlanNote, "error": run.Error}}
}

// verifyTools are the reporter's tools in the verify stage. A positive
// verdict ends the stage; a negative one must be followed by a rejection
// or a question to the editor.
func verifyTools(s *story) []Tool {
	tools := pick(s.tools(), "search_news", "read_source", "web_search", "read_page", "find_similar_stories", "get_article",
		"remember_fact", "record_verification", "reject_story", "ask_editor")
	for i := range tools {
		if tools[i].Spec.Name == "record_verification" {
			tools[i].Terminal = true
			tools[i].Ends = func() bool { return s.verified != nil && *s.verified }
		}
	}
	return tools
}
