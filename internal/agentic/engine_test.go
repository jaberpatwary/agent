package agentic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"bangla-news-aggregator/internal/ai"
)

// scriptedModel replays a fixed list of turns, like a model would produce them.
func scriptedModel(turns ...ai.ChatMessage) (ChatFunc, *[][]ai.ChatMessage) {
	seen := &[][]ai.ChatMessage{}
	i := 0
	return func(_ context.Context, _ string, msgs []ai.ChatMessage, _ []ai.ToolSpec, _ int) (ai.ChatResult, error) {
		*seen = append(*seen, append([]ai.ChatMessage(nil), msgs...))
		if i >= len(turns) {
			return ai.ChatResult{}, errors.New("script exhausted")
		}
		t := turns[i]
		i++
		return ai.ChatResult{Message: t, Provider: "fake", Model: "m", Tokens: 100}, nil
	}, seen
}

func call(name, args string) ai.ChatMessage {
	return ai.ChatMessage{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: name + "-1", Name: name, Args: json.RawMessage(args)}}}
}

func TestEngineLetsTheModelChooseToolsUntilATerminalDecision(t *testing.T) {
	var searched string
	tools := []Tool{
		{Spec: ai.ToolSpec{Name: "search_news"}, Run: func(_ context.Context, a json.RawMessage) (string, error) {
			var p struct{ Query string }
			_ = json.Unmarshal(a, &p)
			searched = p.Query
			return "3 outlets reported it", nil
		}},
		{Spec: ai.ToolSpec{Name: "submit_article"}, Terminal: true, Run: func(context.Context, json.RawMessage) (string, error) {
			return "saved", nil
		}},
	}
	chat, seen := scriptedModel(
		call("search_news", `{"query":"dhaka fire"}`),
		call("submit_article", `{"decision":"publish"}`),
	)
	var trace []StepRecord
	e := Engine{Chat: chat, Tools: tools, MaxTurns: 5, Record: func(r StepRecord) { trace = append(trace, r) }}
	out := e.Run(context.Background(), "sys", "goal")

	if !out.Finished || out.Tool != "submit_article" || out.Turns != 2 || out.Tokens != 200 {
		t.Fatalf("unexpected outcome %+v", out)
	}
	if searched != "dhaka fire" {
		t.Fatalf("tool did not get the model's arguments, got %q", searched)
	}
	// The second model turn must have seen the first tool's result.
	second := (*seen)[1]
	last := second[len(second)-1]
	if last.Role != "tool" || last.Content != "3 outlets reported it" || last.ToolCallID != "search_news-1" {
		t.Fatalf("tool result not fed back to the model: %+v", last)
	}
	if trace[len(trace)-1].Kind != "final" {
		t.Fatalf("trace should end with the final decision: %+v", trace)
	}
}

func TestEngineFeedsToolErrorsBackSoTheModelCanCorrectItself(t *testing.T) {
	attempts := 0
	tools := []Tool{{Spec: ai.ToolSpec{Name: "submit_article"}, Terminal: true, Run: func(context.Context, json.RawMessage) (string, error) {
		attempts++
		if attempts == 1 {
			return "", errors.New("draft has not passed review")
		}
		return "saved", nil
	}}}
	chat, seen := scriptedModel(call("submit_article", `{}`), call("submit_article", `{}`))
	out := (&Engine{Chat: chat, Tools: tools, MaxTurns: 5}).Run(context.Background(), "sys", "goal")
	if !out.Finished || attempts != 2 {
		t.Fatalf("a refused terminal call must not end the run; outcome %+v attempts %d", out, attempts)
	}
	firstResult := (*seen)[1][len((*seen)[1])-1]
	if firstResult.Content != "ERROR: draft has not passed review" {
		t.Fatalf("guardrail error should reach the model, got %q", firstResult.Content)
	}
}

func TestEngineStopsAtGuardrails(t *testing.T) {
	loop := []Tool{{Spec: ai.ToolSpec{Name: "search_news"}, Run: func(context.Context, json.RawMessage) (string, error) { return "x", nil }}}
	chat, _ := scriptedModel(call("search_news", `{}`), call("search_news", `{}`), call("search_news", `{}`), call("search_news", `{}`))
	if out := (&Engine{Chat: chat, Tools: loop, MaxTurns: 3}).Run(context.Background(), "s", "g"); out.Stopped != "max_turns" || out.Turns != 3 {
		t.Fatalf("expected max_turns stop, got %+v", out)
	}
	chat, _ = scriptedModel(call("search_news", `{}`), call("search_news", `{}`), call("search_news", `{}`))
	if out := (&Engine{Chat: chat, Tools: loop, MaxTurns: 10, TokenBudget: 200}).Run(context.Background(), "s", "g"); out.Stopped != "token_budget" {
		t.Fatalf("expected token_budget stop, got %+v", out)
	}
	talk := ai.ChatMessage{Role: "assistant", Content: "I think it is fine."}
	chat, _ = scriptedModel(talk, talk, talk, talk)
	if out := (&Engine{Chat: chat, Tools: loop, MaxTurns: 10}).Run(context.Background(), "s", "g"); out.Stopped != "no_action" {
		t.Fatalf("an agent that never acts must be stopped, got %+v", out)
	}
}
