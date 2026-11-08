package agentic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

// Newsroom is what the agent's tools need from the rest of the app
// (implemented by *cron.Worker).
type Newsroom interface {
	RunSettings() (houseStyle string, sensitive map[string]bool)
	SearchArticles(query string, limit int) ([]domain.Article, error)
	GetArticle(id uuid.UUID) (*domain.Article, error)
	SaveExternal(ctx context.Context, in cron.ExternalArticle) (*domain.Article, error)
	UpdateExternal(ctx context.Context, id uuid.UUID, in cron.ExternalArticle, note string, versions repository.AgentRepository) (*domain.Article, error)
	RecordJourney(in cron.JourneyUpdate) (*domain.NewsJourney, error)
	AIText(ctx context.Context, req ai.GatewayRequest) (ai.Result, error)
}

// NewsHit is one result of a news search.
type NewsHit struct {
	Title, Source, URL string
	Published          time.Time
}

type (
	SearchFunc func(ctx context.Context, query, lang string) ([]NewsHit, error)
	ReadFunc   func(ctx context.Context, url string) (string, error)
)

type draft struct {
	Title, Body, Summary string
	Tags                 []string
}

type verdict struct {
	Passed   bool           `json:"passed"`
	Problems []string       `json:"hard_rule_problems,omitempty"`
	Issues   []string       `json:"reviewer_issues,omitempty"`
	Scores   map[string]int `json:"scores,omitempty"`
	Check    DraftCheck     `json:"measurements"`
	Next     string         `json:"next"`
}

// story is the agent's working state for one news item; the tools read and
// update it, and the guardrails are enforced against it.
type story struct {
	item        cron.PendingItem
	nr          Newsroom
	versions    repository.AgentRepository
	searchNews  SearchFunc
	readPage    ReadFunc
	setJourney  func(fields map[string]any)
	outlets     []string
	autopublish bool

	searches int
	sources  []cron.JourneySource
	verified *bool
	draft    *draft
	review   *verdict
	reviews  int
	asked    bool
	saved    *domain.Article

	// Measured by code, not claimed by the model: outlets whose search hits
	// match this story's headline, and every outlet seen in any search.
	matched map[string]bool
	seen    map[string]bool
	addFact func(fact string) error
	written *draft // last draft produced by write_draft
	latest  *draft // newest draft from either tool: what an editor gets if the story is held
	facts   string // current facts, given to the writer too
}

const maxReviews = 4

func obj(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{} // Groq and Cohere reject "required": null
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}
func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func strList(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

func (s *story) tools() []Tool {
	return []Tool{
		{Spec: ai.ToolSpec{Name: "search_news", Description: "Search recent news coverage (Google News). Use 3-6 key Bangla words from the headline (names, place, event); long or English queries usually find nothing for Bangladeshi news. Shows which outlets carry the same story. Free; prefer this over web_search.",
			Parameters: obj(map[string]any{"query": str("search words, usually the key names, place and event"), "language": str("bn (default) or en")}, "query")},
			Run: s.toolSearchNews},
		{Spec: ai.ToolSpec{Name: "read_source", Description: "Read the full original story text (the goal message shows only its beginning). write_draft and review_draft already use the full text.",
			Parameters: obj(map[string]any{})},
			Run: func(context.Context, json.RawMessage) (string, error) {
				return clip(plainText(s.item.Content), 9000), nil
			}},
		{Spec: ai.ToolSpec{Name: "web_search", Description: "General web search with an AI summary of results. Slower and limited — use only when search_news is not enough (e.g. checking a person's current position).",
			Parameters: obj(map[string]any{"question": str("what you want to find out")}, "question")},
			Run: s.toolWebSearch},
		{Spec: ai.ToolSpec{Name: "read_page", Description: "Read the main text of a news page (e.g. another outlet's report found by search) to collect extra facts.",
			Parameters: obj(map[string]any{"url": str("http(s) URL")}, "url")},
			Run: s.toolReadPage},
		{Spec: ai.ToolSpec{Name: "find_similar_stories", Description: "Search this portal's own archive. Use it to detect duplicates, or a published story this item should update.",
			Parameters: obj(map[string]any{"query": str("key words of the event")}, "query")},
			Run: s.toolFindSimilar},
		{Spec: ai.ToolSpec{Name: "get_article", Description: "Read one of the portal's existing articles in full (to decide whether to update it).",
			Parameters: obj(map[string]any{"article_id": str("id from find_similar_stories")}, "article_id")},
			Run: s.toolGetArticle},
		{Spec: ai.ToolSpec{Name: "record_verification", Description: "Record your fact-check verdict and the independent outlets that corroborate the story. Required before submit_article.",
			Parameters: obj(map[string]any{
				"is_real":               map[string]any{"type": "boolean", "description": "true if the evidence supports the story"},
				"confidence":            str("high | medium | low"),
				"reason":                str("short reason in Bangla, citing what you found"),
				"corroborating_outlets": strList("names of other outlets that reported the same event"),
			}, "is_real", "reason")},
			Run: s.toolRecordVerification},
		{Spec: ai.ToolSpec{Name: "write_draft", Description: "Have the desk writer rewrite the whole source passage by passage in our own words — full length, every fact kept, no copied sentences — and review it at once (hard rules + independent AI editor). If the review passes, submit_article next. Call again with notes to fix what the review reported.",
			Parameters: obj(map[string]any{"notes": str("optional instructions, e.g. extra verified facts to weave in, or what the last review asked to fix")})},
			Run: s.toolWriteDraft},
		{Spec: ai.ToolSpec{Name: "review_draft", Description: "Check a draft you wrote or corrected yourself (write_draft already reviews its own drafts). Runs hard rules (length, copying, outlet names, numbers, new headline) and an independent AI editor. Also use it to send a corrected title/summary or numbers_not_in_story for the written draft (use_written_draft=true).",
			Parameters: obj(map[string]any{
				"title":                    str("new headline in your own words (Bangla)"),
				"body":                     str("the full article as HTML <p> paragraphs, optional <h3> subheadings"),
				"summary":                  str("3-4 sentence plain-text summary (Bangla)"),
				"tags":                     strList("3-5 specific topic tags"),
				"facts_from_other_sources": str("any facts you added from other outlets' reports, so the editor can verify them; empty if none"),
				"numbers_not_in_story":     strList("numbers the hard rules flagged that are NOT facts of this story (times, menu items, other headlines in the scraped page); the editor will check this claim"),
				"use_written_draft":        map[string]any{"type": "boolean", "description": "true to review the draft from write_draft instead of sending title/body yourself"},
			})},
			Run: s.toolReviewDraft},
		{Spec: ai.ToolSpec{Name: "submit_article", Description: "FINAL: save the last reviewed draft. decision=publish needs a passed review and a positive verification; decision=hold keeps it for a human editor. Set update_article_id to replace an existing article with this newer version of the same developing story.",
			Parameters: obj(map[string]any{
				"decision":          str("publish | hold"),
				"note":              str("one or two sentences for the editor: what you verified and decided"),
				"update_article_id": str("optional: id of the existing article to update"),
			}, "decision", "note")},
			Run: s.toolSubmit, Terminal: true},
		{Spec: ai.ToolSpec{Name: "reject_story", Description: "FINAL: do not publish. kind=fake needs search evidence; duplicate = already covered with nothing new; not_news = not a news story; low_quality = too little verifiable content.",
			Parameters: obj(map[string]any{
				"kind":        str("fake | duplicate | not_news | low_quality"),
				"explanation": str("short reason in Bangla"),
				"evidence":    strList("what you found: outlet names, URLs or archive article ids"),
			}, "kind", "explanation")},
			Run: s.toolReject, Terminal: true},
		{Spec: ai.ToolSpec{Name: "ask_editor", Description: "FINAL for now: pause this story and ask a human editor a specific question (e.g. legal risk, an unverifiable serious allegation). The answer will be given to you next time.",
			Parameters: obj(map[string]any{
				"question": str("the precise question, in Bangla"),
				"reason":   str("legal_risk | unverifiable_allegation | sensitive_content"),
			}, "question", "reason")},
			Run: s.toolAskEditor, Terminal: true},
		{Spec: ai.ToolSpec{Name: "remember_fact", Description: "Save a current fact for future stories (e.g. who now holds an office) after search results from at least 2 outlets confirmed it. Use when your memory turned out to be outdated.",
			Parameters: obj(map[string]any{
				"fact":    str("one short sentence in Bangla, e.g. who holds which position now"),
				"outlets": strList("names of the outlets in your search results that state it (at least 2)"),
			}, "fact", "outlets")},
			Run: s.toolRememberFact},
	}
}

func (s *story) toolSearchNews(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct{ Query, Language string }
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.Query) == "" {
		return "", errors.New("query is empty")
	}
	if a.Language != "en" {
		a.Language = "bn"
	}
	hits, err := s.searchNews(ctx, a.Query, a.Language)
	if err != nil {
		return "", fmt.Errorf("news search failed: %v", err)
	}
	s.searches++
	retried := ""
	if len(hits) == 0 {
		// Agents often over-specify queries; retry once with the headline's own key words.
		if q := headlineQuery(s.item.Title); q != "" && q != a.Query {
			if more, err2 := s.searchNews(ctx, q, "bn"); err2 == nil && len(more) > 0 {
				hits, retried = more, q
			}
		}
	}
	if len(hits) == 0 {
		return "No coverage found for this query. Try 3-6 key Bangla words from the headline.", nil
	}
	var b strings.Builder
	if retried != "" {
		fmt.Fprintf(&b, "Your query found nothing; searched the headline words instead: %q\n", retried)
	}
	fmt.Fprintf(&b, "%d results:\n", len(hits))
	for i, h := range hits {
		ago := "unknown time"
		if !h.Published.IsZero() {
			ago = h.Published.In(dhaka()).Format("2006-01-02 15:04")
		}
		link := ""
		if i < 3 {
			link = " " + h.URL
		}
		fmt.Fprintf(&b, "%d. %s — %s (%s)%s\n", i+1, h.Title, h.Source, ago, link)
		s.addSource(h.Source, h.URL)
		s.noteHit(h)
	}
	fmt.Fprintf(&b, "\nOther outlets confirmed (by headline match) to carry this same story so far: %d\n", s.corroboration())
	return b.String(), nil
}

// noteHit records which outlet a search hit came from and whether its
// headline is about the same story.
func (s *story) noteHit(h NewsHit) {
	name := strings.TrimSpace(h.Source)
	if name == "" || strings.EqualFold(name, s.item.SourceName) {
		return
	}
	if s.seen == nil {
		s.seen, s.matched = map[string]bool{}, map[string]bool{}
	}
	s.seen[name] = true
	if SameStory(s.item.Title, h.Title) {
		s.matched[name] = true
	}
}

// corroboration counts independent outlets carrying the same story: search
// hits with a matching headline plus copies found in the portal's own feeds.
func (s *story) corroboration() int {
	return len(s.matched) + len(s.item.RelatedURLs)
}

func (s *story) matchedNames() string {
	names := make([]string, 0, len(s.matched))
	for n := range s.matched {
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

func (s *story) addSource(name, url string) {
	if name == "" || strings.EqualFold(name, s.item.SourceName) {
		return
	}
	for _, x := range s.sources {
		if x.Name == name {
			return
		}
	}
	s.sources = append(s.sources, cron.JourneySource{Name: name, URL: url})
}

func (s *story) toolWebSearch(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct{ Question string }
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	res, err := s.nr.AIText(ctx, ai.GatewayRequest{Task: "cross_check", Search: true, MaxTokens: 1500,
		System: "Search the web and answer concisely. Name the outlets and include URLs. Say plainly if you found nothing.",
		User:   a.Question})
	if err != nil {
		return "", fmt.Errorf("web search unavailable right now (%v) — use search_news instead", shortErr(err))
	}
	s.searches++
	return res.Text, nil
}

func (s *story) toolReadPage(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct{ URL string }
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	text, err := s.readPage(ctx, a.URL)
	if err != nil {
		return "", err
	}
	return text, nil
}

func (s *story) toolFindSimilar(_ context.Context, raw json.RawMessage) (string, error) {
	var a struct{ Query string }
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	list, err := s.nr.SearchArticles(a.Query, 8)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "No similar stories in the portal.", nil
	}
	var b strings.Builder
	for _, x := range list {
		fmt.Fprintf(&b, "- id=%s | %s | %s | %s\n", x.ID, x.Status, x.PublishedAt.In(dhaka()).Format("2006-01-02 15:04"), x.Title)
	}
	return b.String(), nil
}

func (s *story) toolGetArticle(_ context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		ArticleID string `json:"article_id"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	id, err := uuid.Parse(strings.TrimSpace(a.ArticleID))
	if err != nil {
		return "", errors.New("article_id is not a valid id")
	}
	art, err := s.nr.GetArticle(id)
	if err != nil || art == nil {
		return "", errors.New("article not found")
	}
	return fmt.Sprintf("title: %s\nstatus: %s\npublished: %s\n\n%s", art.Title, art.Status,
		art.PublishedAt.In(dhaka()).Format("2006-01-02 15:04"), clip(plainText(art.Content), 4000)), nil
}

func (s *story) toolRecordVerification(_ context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		IsReal     bool     `json:"is_real"`
		Confidence string   `json:"confidence"`
		Reason     string   `json:"reason"`
		Outlets    []string `json:"corroborating_outlets"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	if s.searches == 0 {
		return "", errors.New("verify with search_news (or web_search) before recording a verdict — never judge from memory alone")
	}
	for _, o := range a.Outlets {
		s.addSource(strings.TrimSpace(o), "")
	}
	isFake := !a.IsReal
	reason := strings.TrimSpace(a.Reason)
	if a.Confidence != "" {
		reason += " (আস্থা: " + a.Confidence + ")"
	}
	if _, err := s.nr.RecordJourney(cron.JourneyUpdate{NewsID: s.item.NewsID, Stage: "fake_check", IsFake: &isFake, Reason: reason}); err != nil {
		return "", err
	}
	if _, err := s.nr.RecordJourney(cron.JourneyUpdate{NewsID: s.item.NewsID, Stage: "cross_check", Sources: s.sources}); err != nil {
		return "", err
	}
	s.verified = &a.IsReal
	if !a.IsReal {
		return "Recorded: NOT supported by evidence. If you are confident it is false, call reject_story(kind=fake); if unsure, ask_editor.", nil
	}
	next := "Next: write_draft (it reviews the draft too)."
	if s.review != nil && s.review.Passed {
		next = "The current draft passed its review: submit_article next."
	}
	return fmt.Sprintf("Recorded: verified, %d corroborating outlets. %s", len(s.sources), next), nil
}

const reviewerRules = `You are a strict senior news editor checking a Bangla article written by an AI reporter against its source material.
Reply ONLY with JSON: {"passed": true/false, "scores": {"accuracy":1-5, "completeness":1-5, "originality":1-5, "language":1-5}, "issues": ["specific problem and how to fix it", ...]}.
Fail (passed=false) if ANY of these: a fact, number, name or quote is not in the source, the listed extra facts or the CURRENT FACTS; an important fact from the source is missing; whole sentences are copied unchanged from the source outside quotation marks; any news outlet, news agency, reporter or website is named; the body repeats the same information (the summary restating the body is normal, not repetition); the Bangla is unclear or ungrammatical; it adds opinion.
Judge facts ONLY against the source and the CURRENT FACTS — never against your own memory, which is out of date. If the source gives someone a post, title or party role (e.g. প্রধানমন্ত্রী, মন্ত্রী, ক্ষমতাসীন দল), that is correct and the draft must keep it; never call it an error.
Word-for-word copying is also measured by code; a sentence that says the same thing in new words is fine — do not fail a draft only because its paraphrase is close.
Write issues in Bangla or English, each actionable.`

// reviewerSystem is the reviewer's prompt with the editors' current facts,
// so it does not "correct" today's office-holders from outdated memory.
func reviewerSystem(facts string) string { return reviewerRules + factsBlock(facts) }

func (s *story) toolReviewDraft(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Title, Body, Summary string
		Tags                 []string
		UseWritten           bool     `json:"use_written_draft"`
		Extra                string   `json:"facts_from_other_sources"`
		IgnoreNumbers        []string `json:"numbers_not_in_story"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	d := &draft{Title: strings.TrimSpace(a.Title), Body: strings.TrimSpace(a.Body), Summary: strings.TrimSpace(a.Summary), Tags: a.Tags}
	if a.UseWritten || d.Body == "" {
		if s.written == nil {
			return "", errors.New("no draft yet: call write_draft first, or pass title and body")
		}
		w := *s.written
		if d.Title != "" {
			w.Title = d.Title
		}
		if d.Summary != "" {
			w.Summary = d.Summary
		}
		if len(d.Tags) > 0 {
			w.Tags = d.Tags
		}
		d = &w
	}
	if d.Title == "" || d.Body == "" {
		return "", errors.New("title and body are required")
	}
	v, err := s.runReview(ctx, d, a.Extra, a.IgnoreNumbers)
	if err != nil {
		return "", err
	}
	out, _ := json.MarshalIndent(v, "", " ")
	return string(out), nil
}

// runReview checks a draft with the hard rules (code) and an independent AI
// editor, and makes it the story's current draft.
func (s *story) runReview(ctx context.Context, d *draft, extra string, ignoreNumbers []string) (*verdict, error) {
	if s.reviews >= maxReviews {
		return nil, fmt.Errorf("review limit (%d) reached — submit_article with decision=hold for an editor, or ask_editor", maxReviews)
	}
	s.reviews++
	s.draft, s.latest = d, d
	v := &verdict{Check: CheckDraft(s.item.Title, s.item.Content, d.Body, s.outlets, ignoreNumbers)}
	if p := headlineProblem(s.item.Title, d.Title); p != "" {
		v.Check.Problems = append(v.Check.Problems, p)
	}
	v.Problems = v.Check.Problems

	user := fmt.Sprintf("SOURCE TITLE: %s\n\nSOURCE TEXT:\n%s\n\nEXTRA FACTS THE REPORTER TOOK FROM OTHER OUTLETS:\n%s\n\nNUMBERS THE REPORTER SAYS ARE NOT PART OF THE STORY (page furniture — fail the draft if any of them is actually a fact of the story):\n%s\n\nDRAFT TITLE: %s\nDRAFT SUMMARY: %s\nDRAFT BODY:\n%s",
		s.item.Title, clip(plainText(s.item.Content), 14000), orNone(extra), orNone(strings.Join(ignoreNumbers, ", ")), d.Title, d.Summary, plainText(d.Body))
	res, err := s.nr.AIText(ctx, ai.GatewayRequest{Task: "review", JSON: true, MaxTokens: 1500, System: reviewerSystem(s.facts), User: user})
	llmPassed := true
	if err != nil {
		v.Issues = append(v.Issues, "AI editor unavailable right now ("+shortErr(err)+"); only the hard rules were checked")
	} else {
		var r struct {
			Passed bool           `json:"passed"`
			Scores map[string]int `json:"scores"`
			Issues []string       `json:"issues"`
		}
		if jerr := json.Unmarshal([]byte(extractJSON(res.Text)), &r); jerr != nil {
			v.Issues = append(v.Issues, "AI editor reply was unreadable; only the hard rules were checked")
		} else {
			llmPassed, v.Scores, v.Issues = r.Passed, r.Scores, r.Issues
		}
	}
	v.Passed = v.Check.OK() && llmPassed
	switch {
	case v.Passed:
		v.Next = "Passed. If verification is recorded as real, call submit_article."
	case s.reviews >= maxReviews:
		v.Next = "Still failing and no reviews left: submit_article with decision=hold, or ask_editor."
	default:
		v.Next = fmt.Sprintf("Fix every problem above: write_draft again with notes on what to fix, or review_draft with your own corrected title/body (%d of %d reviews used).", s.reviews, maxReviews)
	}
	s.review = v
	_, _ = s.nr.RecordJourney(cron.JourneyUpdate{NewsID: s.item.NewsID, Stage: "rewritten", RewrittenTitle: d.Title, RewrittenContent: d.Body})
	return v, nil
}

func (s *story) toolSubmit(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Decision        string `json:"decision"`
		Note            string `json:"note"`
		UpdateArticleID string `json:"update_article_id"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	if s.draft == nil || s.review == nil {
		return "", errors.New("no reviewed draft yet: write the full article and call review_draft first")
	}
	if s.verified == nil {
		return "", errors.New("verification not recorded: search, then call record_verification")
	}
	if !*s.verified {
		return "", errors.New("your verification says the story is not supported — reject_story or ask_editor instead")
	}
	decision := strings.ToLower(strings.TrimSpace(a.Decision))
	if decision != "publish" && decision != "hold" {
		return "", errors.New("decision must be publish or hold")
	}
	if decision == "publish" && !s.review.Passed {
		return "", errors.New("the last review did not pass: fix the problems and call review_draft again, or submit with decision=hold")
	}
	// Only the reviewed draft is ever published; a held story gives the
	// editor the newest one (often written after the last review).
	d := s.draft
	if decision == "hold" && s.latest != nil {
		d = s.latest
	}
	in := cron.ExternalArticle{
		NewsID: s.item.NewsID, SourceURL: s.item.SourceURL, RelatedURLs: s.item.RelatedURLs,
		Title: d.Title, Body: d.Body, Summary: d.Summary, Tags: d.Tags,
		ImageURL: s.item.ImageURL, SourceName: s.item.SourceName, Category: s.item.Category, PublishedAt: s.item.PublishedAt,
		OriginalTitle: s.item.Title, OriginalContent: s.item.Content,
		Corroborated: s.corroboration(),
	}
	switch {
	case decision == "hold":
		in.ForceReview = "এজেন্ট সম্পাদকের অনুমোদনের জন্য রেখেছে: " + strings.TrimSpace(a.Note)
	case !s.autopublish:
		in.ForceReview = "স্বয়ংক্রিয় প্রকাশ বন্ধ — সম্পাদকের অনুমোদন প্রয়োজন"
	}

	var art *domain.Article
	var err error
	if id := strings.TrimSpace(a.UpdateArticleID); id != "" {
		uid, perr := uuid.Parse(id)
		if perr != nil {
			return "", errors.New("update_article_id is not a valid id")
		}
		art, err = s.nr.UpdateExternal(ctx, uid, in, a.Note, s.versions)
	} else {
		art, err = s.nr.SaveExternal(ctx, in)
	}
	if errors.Is(err, repository.ErrDuplicateArticle) {
		return "", errors.New("this story already exists in the portal — use find_similar_stories, then update it (update_article_id) or reject_story(kind=duplicate)")
	}
	if err != nil {
		return "", err
	}
	s.saved = art
	msg := fmt.Sprintf("Saved. article_id=%s status=%s corroborating_outlets=%d", art.ID, art.Status, s.corroboration())
	if art.ReviewReason != "" {
		msg += " (held for editor: " + art.ReviewReason + ")"
	}
	return msg, nil
}

func (s *story) toolReject(_ context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Kind, Explanation string
		Evidence          []string
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	kind := strings.ToLower(strings.TrimSpace(a.Kind))
	reason := strings.TrimSpace(a.Explanation)
	if len(a.Evidence) > 0 {
		reason += " | প্রমাণ: " + strings.Join(a.Evidence, "; ")
	}
	switch kind {
	case "fake":
		if n := s.corroboration(); n >= cron.MinCorroboration {
			return "", fmt.Errorf("%d other outlets (%s) carry this same story, so it is not fake. If a detail contradicts what you remember, the outlets are right and your memory is outdated — write the story, or hold it", n, s.matchedNames())
		}
		if s.searches == 0 || len(a.Evidence) == 0 {
			return "", errors.New("calling a story fake needs evidence: search first and list what you found in evidence")
		}
		isFake := true
		if _, err := s.nr.RecordJourney(cron.JourneyUpdate{NewsID: s.item.NewsID, Stage: "fake_check", IsFake: &isFake, Reason: reason}); err != nil {
			return "", err
		}
		s.setJourney(map[string]any{"published_status": "rejected"})
	case "duplicate":
		s.setJourney(map[string]any{"published_status": "duplicate"})
	case "not_news", "low_quality":
		s.setJourney(map[string]any{"published_status": "rejected"})
	default:
		return "", errors.New("kind must be fake, duplicate, not_news or low_quality")
	}
	return "Rejected (" + kind + "): " + reason, nil
}

func (s *story) toolAskEditor(_ context.Context, raw json.RawMessage) (string, error) {
	var a struct{ Question, Reason string }
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	switch strings.TrimSpace(a.Reason) {
	case "legal_risk", "unverifiable_allegation", "sensitive_content":
	default:
		return "", errors.New("ask_editor is only for legal_risk, unverifiable_allegation or sensitive_content. If the review keeps failing, fix it or submit_article with decision=hold; if the story contradicts your memory, trust the sources and CURRENT FACTS")
	}
	q := strings.TrimSpace(a.Question)
	if q == "" {
		return "", errors.New("question is empty")
	}
	s.asked = true
	s.setJourney(map[string]any{"published_status": "needs_editor", "agent_question": q})
	return "Question sent to the editor; this story is paused until they answer.", nil
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}

func shortErr(err error) string {
	return clip(err.Error(), 160)
}

// extractJSON trims code fences / chatter around a JSON object.
func extractJSON(s string) string {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return s
	}
	return s[start : end+1]
}

func dhaka() *time.Location {
	if loc, err := time.LoadLocation("Asia/Dhaka"); err == nil {
		return loc
	}
	return time.UTC
}

func (s *story) toolRememberFact(_ context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Fact    string
		Outlets []string
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	fact := strings.TrimSpace(a.Fact)
	if fact == "" || len([]rune(fact)) > 240 {
		return "", errors.New("fact must be one short sentence")
	}
	var confirmed []string
	for _, o := range a.Outlets {
		if s.seen[strings.TrimSpace(o)] {
			confirmed = append(confirmed, strings.TrimSpace(o))
		}
	}
	if len(confirmed) < 2 {
		return "", errors.New("a fact needs at least 2 outlets that actually appeared in your search results in this story")
	}
	if s.addFact == nil {
		return "", errors.New("fact memory is not available")
	}
	line := fmt.Sprintf("%s — সূত্র: %s (%s)", fact, strings.Join(confirmed, ", "), time.Now().In(dhaka()).Format("2006-01-02"))
	if err := s.addFact(line); err != nil {
		return "", err
	}
	return "Remembered for future stories: " + line, nil
}

// observe gathers, without a model call, what the agent needs on its first
// turn anyway: news coverage of the headline (corroboration) and the
// portal's own archive (duplicates). The agent can still search further.
func (s *story) observe(ctx context.Context, record func(StepRecord)) string {
	q := headlineQuery(s.item.Title)
	if q == "" {
		return ""
	}
	args, _ := json.Marshal(map[string]string{"query": q})
	var b strings.Builder
	b.WriteString("\nALREADY GATHERED BY THE DESK (do not repeat these calls):\n")
	for _, t := range []struct {
		name string
		run  func(context.Context, json.RawMessage) (string, error)
	}{{"search_news", s.toolSearchNews}, {"find_similar_stories", s.toolFindSimilar}} {
		record(StepRecord{Kind: "tool", Tool: t.name, Input: string(args)})
		res, err := t.run(ctx, args)
		if err != nil {
			res = "ERROR: " + err.Error()
		}
		res = clip(res, 2500)
		record(StepRecord{Kind: "result", Tool: t.name, Output: res})
		fmt.Fprintf(&b, "\n[%s %q]\n%s\n", t.name, q, res)
	}
	return b.String()
}

// headlineQuery builds a short search query from a headline's meaningful words.
func headlineQuery(title string) string {
	words := strings.FieldsFunc(title, func(r rune) bool {
		return r == ' ' || r == ',' || r == ':' || r == '।' || r == '‘' || r == '’' || r == '"' || r == '\'' || r == '-' || r == '—'
	})
	var keep []string
	for _, w := range words {
		if len([]rune(w)) >= 3 && !headlineStopwords[w] {
			keep = append(keep, w)
		}
		if len(keep) == 5 {
			break
		}
	}
	return strings.Join(keep, " ")
}
