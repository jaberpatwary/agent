// Package agentic is the portal's autonomous news desk. Instead of a fixed
// pipeline (verify -> cross-check -> rewrite -> review -> save, always in that
// order), a model is given a goal, a set of tools and guardrails, and decides
// for itself what to do next: which searches to run, which pages to read,
// whether the story is a duplicate or an update, when its draft is good
// enough, and whether to publish, hold, reject, or ask a human editor.
package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"bangla-news-aggregator/internal/ai"
)

// ChatFunc produces the model's next turn (backed by ai.Gateway.Chat).
type ChatFunc func(ctx context.Context, system string, msgs []ai.ChatMessage, tools []ai.ToolSpec, maxTokens int) (ai.ChatResult, error)

// Tool is one action the agent may take.
type Tool struct {
	Spec     ai.ToolSpec
	Run      func(ctx context.Context, args json.RawMessage) (string, error)
	Terminal bool // a successful call ends the agent's work on the story
}

// StepRecord is one entry of the agent's trace.
type StepRecord struct {
	Kind     string // thought | tool | result | error | final
	Tool     string
	Input    string
	Output   string
	Provider string
	Model    string
	Tokens   int
}

// Engine runs the observe -> decide -> act loop for one goal.
type Engine struct {
	Chat        ChatFunc
	Tools       []Tool
	MaxTurns    int // model turns before the agent is stopped
	TokenBudget int // total tokens before the agent is stopped (0 = unlimited)
	MaxTokens   int // per model turn
	Record      func(StepRecord)
}

// Outcome says how the loop ended.
type Outcome struct {
	Finished bool   // a terminal tool succeeded
	Tool     string // which terminal tool ended it
	Result   string // that tool's result
	Turns    int
	Tokens   int
	Stopped  string // when not finished: max_turns | token_budget | provider_error | no_action
	Detail   string
}

const maxToolOutput = 6000

func (e *Engine) record(r StepRecord) {
	if e.Record != nil {
		e.Record(r)
	}
}

// Run gives the agent its goal and lets it work until it reaches a final
// decision or hits a guardrail.
func (e *Engine) Run(ctx context.Context, system, goal string) Outcome {
	specs := make([]ai.ToolSpec, 0, len(e.Tools))
	byName := make(map[string]Tool, len(e.Tools))
	for _, t := range e.Tools {
		specs = append(specs, t.Spec)
		byName[t.Spec.Name] = t
	}
	maxTokens := e.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 6000
	}

	msgs := []ai.ChatMessage{{Role: "user", Content: goal}}
	out := Outcome{}
	idleTurns := 0
	for out.Turns < e.MaxTurns {
		if e.TokenBudget > 0 && out.Tokens >= e.TokenBudget {
			out.Stopped, out.Detail = "token_budget", fmt.Sprintf("used %d of %d tokens", out.Tokens, e.TokenBudget)
			return out
		}
		if ctx.Err() != nil {
			out.Stopped, out.Detail = "provider_error", ctx.Err().Error()
			return out
		}
		res, err := e.Chat(ctx, system, compactHistory(msgs), specs, maxTokens)
		if err != nil {
			e.record(StepRecord{Kind: "error", Output: err.Error()})
			out.Stopped, out.Detail = "provider_error", err.Error()
			return out
		}
		out.Turns++
		out.Tokens += res.Tokens
		msg := res.Message
		msgs = append(msgs, msg)

		calls := make([]string, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			calls = append(calls, tc.Name)
		}
		e.record(StepRecord{Kind: "thought", Output: strings.TrimSpace(msg.Content), Input: strings.Join(calls, ", "),
			Provider: res.Provider, Model: res.Model, Tokens: res.Tokens})

		if len(msg.ToolCalls) == 0 {
			// The model talked instead of acting. Remind it once or twice;
			// an agent that never commits to a decision is stopped.
			idleTurns++
			if idleTurns > 2 {
				out.Stopped, out.Detail = "no_action", "the agent stopped calling tools without a final decision"
				return out
			}
			msgs = append(msgs, ai.ChatMessage{Role: "user", Content: "Keep working by calling tools. You must finish by calling exactly one of: submit_article, reject_story, ask_editor."})
			continue
		}
		idleTurns = 0

		for _, tc := range msg.ToolCalls {
			tool, ok := byName[tc.Name]
			var text string
			var runErr error
			if !ok {
				runErr = fmt.Errorf("unknown tool %q", tc.Name)
			} else {
				e.record(StepRecord{Kind: "tool", Tool: tc.Name, Input: clip(string(tc.Args), 4000)})
				text, runErr = tool.Run(ctx, tc.Args)
			}
			if runErr != nil {
				text = "ERROR: " + runErr.Error()
			}
			text = clip(text, maxToolOutput)
			e.record(StepRecord{Kind: "result", Tool: tc.Name, Output: text})
			msgs = append(msgs, ai.ChatMessage{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Name, Content: text})
			if ok && tool.Terminal && runErr == nil {
				out.Finished, out.Tool, out.Result = true, tc.Name, text
				e.record(StepRecord{Kind: "final", Tool: tc.Name, Output: text})
				return out
			}
		}
	}
	out.Stopped, out.Detail = "max_turns", fmt.Sprintf("reached the limit of %d turns", e.MaxTurns)
	return out
}

// seenToolRunes is how much of a tool result is re-sent once the model has
// already read it in full.
const seenToolRunes = 700

// compactHistory shortens tool results the model already saw in an earlier
// turn. Every turn re-sends the whole conversation, so old search results and
// drafts would otherwise be paid for again on each turn — the main cost of a
// story. The newest results (after the model's last turn) stay complete.
func compactHistory(msgs []ai.ChatMessage) []ai.ChatMessage {
	lastModel := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			lastModel = i
			break
		}
	}
	out := make([]ai.ChatMessage, len(msgs))
	copy(out, msgs)
	for i := 0; i < lastModel; i++ {
		if out[i].Role == "tool" && utf8.RuneCountInString(out[i].Content) > seenToolRunes {
			out[i].Content = string([]rune(out[i].Content)[:seenToolRunes]) + " …[already read; shortened — call the tool again if you need all of it]"
		}
	}
	return out
}

func clip(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + " …[truncated]"
}

// decodeArgs parses a tool's JSON arguments into dst.
func decodeArgs(raw json.RawMessage, dst any) error {
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}
