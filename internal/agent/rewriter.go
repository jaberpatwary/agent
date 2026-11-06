// Package agent implements the agentic news-rewriting pipeline: a two-step
// "draft -> self-review -> finalize" flow that takes a scraped article and
// rewrites it in the portal's own editorial voice before publication,
// instead of simply summarizing the original text.
//
// This is intentionally kept narrow in scope: it does not browse the web,
// call external tools, or verify facts against other sources. Its only
// "tool" is a second pass over its own output. That keeps it cheap, fast,
// and auditable (every attempt — draft and review — is logged to ai_usage),
// while still catching the most common failure mode of single-shot LLM
// rewrites: an invented detail that wasn't in the source.
//
// It is not a replacement for human editorial review. Treat it as a first
// draft generator: articles are still visible and editable/removable from
// /admin/articles after publication.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bangla-news-aggregator/internal/ai"
)

// Result is the outcome of one agentic rewrite pass, plus every provider
// attempt made along the way (draft + review), so the caller can log them
// all to ai_usage regardless of whether the overall rewrite succeeded.
type Result struct {
	Title     string
	Body      string
	Summary   string
	Tags      []string
	FactCheck FactCheckResult
	Attempts  []ai.Result
	Err       error
}

// aiRunner is the subset of *ai.Client the rewriter needs; defined as an
// interface so tests can fake it without hitting real providers.
type aiRunner interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string, maxTokens int) (ai.Result, []ai.Result)
}

// Rewriter drives the draft -> self-review -> finalize flow.
type Rewriter struct {
	client aiRunner
}

func NewRewriter(client *ai.Client) *Rewriter {
	return &Rewriter{client: client}
}

// draftTokenBudget / reviewTokenBudget are generous enough for a full
// Bangla news article (a few hundred words becomes noticeably more tokens
// in Bangla script than in English) plus the JSON wrapper.
const (
	draftTokenBudget  = 1800
	reviewTokenBudget = 2000
	minBodyRunes      = 200 // below this, treat the rewrite as a failure and let the caller fall back

	// Synthesis prompts carry several full source articles, so both the
	// input and the expected output are larger than a single-source rewrite.
	synthesisDraftTokenBudget  = 2600
	synthesisReviewTokenBudget = 2800
)

type rewriteJSON struct {
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
}

// cleanTags trims, drops empties/dupes, and caps the tag list so a model
// that ignores the "3 to 5" instruction doesn't flood an article with noise.
func cleanTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

// Rewrite runs the full agentic pipeline for one article. houseStyle comes
// from the admin-editable setting (see internal/usecase SettingsUsecase);
// title/content are the raw scraped values.
func (r *Rewriter) Rewrite(ctx context.Context, houseStyle, title, content string) Result {
	var attempts []ai.Result
	system := ai.RewriteSystemPrompt(houseStyle)

	// Step 1: draft.
	draftResult, draftAttempts := r.client.Complete(ctx, system, ai.BuildRewriteDraftPrompt(title, content), draftTokenBudget)
	attempts = append(attempts, draftAttempts...)
	if draftResult.Err != nil {
		return Result{Attempts: attempts, Err: fmt.Errorf("agent: draft step failed: %w", draftResult.Err)}
	}
	draft, err := parseRewriteJSON(draftResult.Text)
	if err != nil {
		return Result{Attempts: attempts, Err: fmt.Errorf("agent: draft step returned unparseable JSON: %w", err)}
	}

	// Step 2: self-review against the original source. This is the
	// "agentic" part — the model is asked to check and correct its own
	// work, not just produce one unverified pass.
	draftJSON, _ := json.Marshal(draft)
	reviewResult, reviewAttempts := r.client.Complete(ctx, system, ai.BuildRewriteReviewPrompt(title, content, string(draftJSON)), reviewTokenBudget)
	attempts = append(attempts, reviewAttempts...)

	final := draft
	if reviewResult.Err == nil {
		if reviewed, err := parseRewriteJSON(reviewResult.Text); err == nil {
			final = reviewed
		}
		// If the review step fails to parse, we silently keep the draft
		// rather than failing the whole article — a good unreviewed
		// rewrite beats discarding the work entirely.
	}

	final.Title = strings.TrimSpace(final.Title)
	final.Body = strings.TrimSpace(final.Body)
	final.Summary = strings.TrimSpace(final.Summary)

	if final.Title == "" || countVisibleRunes(final.Body) < minBodyRunes {
		return Result{Attempts: attempts, Err: fmt.Errorf("agent: final output too short or missing a title (body=%d visible chars)", countVisibleRunes(final.Body))}
	}
	if final.Summary == "" {
		final.Summary = truncateRunes(stripTags(final.Body), 280)
	}

	factCheck := CheckFactConsistency([]string{title, content}, final.Body)

	return Result{Title: final.Title, Body: final.Body, Summary: final.Summary, Tags: cleanTags(final.Tags), FactCheck: factCheck, Attempts: attempts}
}

// RewriteMulti is the multi-source counterpart of Rewrite: instead of
// rewriting one article, it synthesizes several publishers' independent
// coverage of the same real-world event (as clustered by internal/cron) into
// a single, more complete article — the feature that differentiates this
// pipeline from a plain per-feed summarizer. The same draft -> self-review
// flow applies, just with every source visible at both steps.
func (r *Rewriter) RewriteMulti(ctx context.Context, houseStyle string, sources []ai.SynthesisSource) Result {
	var attempts []ai.Result
	system := ai.RewriteSystemPrompt(houseStyle)

	draftResult, draftAttempts := r.client.Complete(ctx, system, ai.BuildSynthesisDraftPrompt(sources), synthesisDraftTokenBudget)
	attempts = append(attempts, draftAttempts...)
	if draftResult.Err != nil {
		return Result{Attempts: attempts, Err: fmt.Errorf("agent: synthesis draft step failed: %w", draftResult.Err)}
	}
	draft, err := parseRewriteJSON(draftResult.Text)
	if err != nil {
		return Result{Attempts: attempts, Err: fmt.Errorf("agent: synthesis draft returned unparseable JSON: %w", err)}
	}

	draftJSON, _ := json.Marshal(draft)
	reviewResult, reviewAttempts := r.client.Complete(ctx, system, ai.BuildSynthesisReviewPrompt(sources, string(draftJSON)), synthesisReviewTokenBudget)
	attempts = append(attempts, reviewAttempts...)

	final := draft
	if reviewResult.Err == nil {
		if reviewed, err := parseRewriteJSON(reviewResult.Text); err == nil {
			final = reviewed
		}
	}
	final.Title = strings.TrimSpace(final.Title)
	final.Body = strings.TrimSpace(final.Body)
	final.Summary = strings.TrimSpace(final.Summary)

	if final.Title == "" || countVisibleRunes(final.Body) < minBodyRunes {
		return Result{Attempts: attempts, Err: fmt.Errorf("agent: synthesis output too short or missing a title (body=%d visible chars)", countVisibleRunes(final.Body))}
	}
	if final.Summary == "" {
		final.Summary = truncateRunes(stripTags(final.Body), 280)
	}

	sourceTexts := make([]string, len(sources))
	for i, s := range sources {
		sourceTexts[i] = s.Title + " " + s.Content
	}
	factCheck := CheckFactConsistency(sourceTexts, final.Body)

	return Result{Title: final.Title, Body: final.Body, Summary: final.Summary, Tags: cleanTags(final.Tags), FactCheck: factCheck, Attempts: attempts}
}

// parseRewriteJSON tolerates the common ways small/free LLMs misbehave:
// markdown code fences around the JSON, or stray text before/after it.
func parseRewriteJSON(raw string) (rewriteJSON, error) {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end < start {
		return rewriteJSON{}, fmt.Errorf("no JSON object found in response")
	}
	text = text[start : end+1]

	var parsed rewriteJSON
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return rewriteJSON{}, err
	}
	return parsed, nil
}

var tagStripper = strings.NewReplacer("<p>", "", "</p>", " ", "<br>", " ", "<br/>", " ")

func stripTags(html string) string {
	return strings.Join(strings.Fields(tagStripper.Replace(html)), " ")
}

func countVisibleRunes(html string) int {
	return len([]rune(stripTags(html)))
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
