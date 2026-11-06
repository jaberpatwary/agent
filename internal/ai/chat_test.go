package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGeminiContentsGroupsToolResultsAndSignsForeignCalls(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "verify this story"},
		{Role: "assistant", Content: "searching", ToolCalls: []ToolCall{
			{ID: "a", Name: "search_news", Args: json.RawMessage(`{"query":"x"}`)},
			{ID: "b", Name: "find_similar_stories", Args: json.RawMessage(`{"query":"x"}`)},
		}},
		{Role: "tool", ToolCallID: "a", ToolName: "search_news", Content: "3 outlets"},
		{Role: "tool", ToolCallID: "b", ToolName: "find_similar_stories", Content: "none"},
	}
	got := geminiContents(msgs, "gemini-x")
	if len(got) != 3 {
		t.Fatalf("want user, model, user(results) = 3 turns, got %d", len(got))
	}
	if got[1].Role != "model" || !strings.Contains(string(got[1].Parts), geminiDummySignature) {
		t.Fatalf("a call made by another provider must carry the dummy thought signature: %s", got[1].Parts)
	}
	var results []geminiChatPart
	if err := json.Unmarshal(got[2].Parts, &results); err != nil || len(results) != 2 {
		t.Fatalf("both tool results must go back in one turn: %s", got[2].Parts)
	}
	if results[0].FunctionResponse.Name != "search_news" || results[1].FunctionResponse.Name != "find_similar_stories" {
		t.Fatalf("results matched to the wrong tools: %+v", results)
	}
}

func TestGeminiContentsReplaysOwnPartsOnlyForSameModel(t *testing.T) {
	own := ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{ID: "a", Name: "t"}},
		geminiParts: json.RawMessage(`[{"functionCall":{"name":"t","args":{}},"thoughtSignature":"REAL"}]`), geminiModel: "m1"}
	same := geminiContents([]ChatMessage{own}, "m1")
	if !strings.Contains(string(same[0].Parts), "REAL") {
		t.Fatal("the same model must get its real signature back")
	}
	other := geminiContents([]ChatMessage{own}, "m2")
	if strings.Contains(string(other[0].Parts), "REAL") || !strings.Contains(string(other[0].Parts), geminiDummySignature) {
		t.Fatal("a different model must get the dummy signature, not another model's")
	}
}

func TestOpenAIMessagesCarryToolCallsAndResults(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "search_news", Args: json.RawMessage(`{"query":"q"}`)}}},
		{Role: "tool", ToolCallID: "c1", ToolName: "search_news", Content: "ok"},
	}
	out := openAIMessages("sys", msgs)
	if len(out) != 4 || out[0]["role"] != "system" {
		t.Fatalf("unexpected messages: %v", out)
	}
	calls := out[2]["tool_calls"].([]map[string]any)
	if calls[0]["id"] != "c1" || out[3]["tool_call_id"] != "c1" {
		t.Fatalf("tool call and result must share the id: %v", out)
	}
}
