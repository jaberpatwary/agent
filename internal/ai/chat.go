package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// This file adds tool calling ("function calling") to the Gateway, which is
// what lets an agent decide for itself which action to take next. A
// conversation is kept in one provider-neutral format and translated to
// Gemini, Groq (OpenAI format) or Cohere on every call, so the agent can fall
// back to another provider mid-conversation when one is busy or out of quota.

// ChatMessage is one turn of an agent conversation.
type ChatMessage struct {
	Role       string     `json:"role"` // user | assistant | tool
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // assistant: actions the model asked for
	ToolCallID string     `json:"tool_call_id,omitempty"` // tool: which call this answers
	ToolName   string     `json:"tool_name,omitempty"`    // tool: Gemini matches results by name

	// Gemini returns opaque "thought signatures" that must be sent back
	// unchanged on later turns; keep its raw parts and which model made them.
	geminiParts json.RawMessage
	geminiModel string
}

// ToolCall is one action requested by the model.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ToolSpec describes a tool to the model (parameters are a JSON schema object).
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ChatResult is the model's next turn plus who produced it.
type ChatResult struct {
	Message  ChatMessage
	Provider string
	Model    string
	Tokens   int
}

// geminiDummySignature is the value Gemini documents for function calls in
// history it did not produce itself (e.g. made by another provider).
const geminiDummySignature = "skip_thought_signature_validator"

func (g *Gateway) chatRoutes() []gatewayRoute {
	c := g.cfg
	var out []gatewayRoute
	add := func(provider, key, model string) {
		if key != "" && model != "" {
			out = append(out, gatewayRoute{provider: provider, model: model})
		}
	}
	add("gemini", c.GeminiAPIKey, c.GeminiModel)
	add("gemini", c.GeminiAPIKey, c.GeminiFallbackModel)
	add("groq", c.GroqAPIKey, c.GatewayGroqWriteModel)
	add("cohere", c.CohereAPIKey, c.CohereModel)
	return out
}

// Chat asks the next agent turn, trying providers in order (Gemini, Gemini
// fallback, Groq, Cohere) and skipping any that are cooling down.
func (g *Gateway) Chat(ctx context.Context, system string, msgs []ChatMessage, tools []ToolSpec, maxTokens int) (ChatResult, []Result) {
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	var attempts []Result
	for _, rt := range g.chatRoutes() {
		key := rt.provider + "/" + rt.model
		if g.coolingDown(key) {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		start := time.Now()
		var (
			msg    ChatMessage
			tokens int
			err    error
		)
		switch rt.provider {
		case "gemini":
			msg, tokens, err = g.chatGemini(ctx, rt.model, system, msgs, tools, maxTokens)
		case "groq":
			msg, tokens, err = g.chatOpenAI(ctx, "https://api.groq.com/openai/v1/chat/completions", g.cfg.GroqAPIKey, rt.model, system, msgs, tools, maxTokens)
		case "cohere":
			msg, tokens, err = g.chatCohere(ctx, rt.model, system, msgs, tools, maxTokens)
		}
		if err == nil && strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
			err = errors.New("empty response")
		}
		attempts = append(attempts, Result{Provider: rt.provider, Model: rt.model, Text: msg.Content, TokensUsed: tokens, Duration: time.Since(start), Err: err})
		if err == nil {
			return ChatResult{Message: msg, Provider: rt.provider, Model: rt.model, Tokens: tokens}, attempts
		}
		g.coolDown(key, err)
	}
	if len(attempts) == 0 {
		return ChatResult{}, []Result{{Err: errors.New("no AI provider available (all cooling down or not configured)")}}
	}
	return ChatResult{}, attempts
}

// ---------------- Gemini ----------------

type geminiFnCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type geminiChatPart struct {
	Text             string          `json:"text,omitempty"`
	Thought          bool            `json:"thought,omitempty"`
	ThoughtSignature string          `json:"thoughtSignature,omitempty"`
	FunctionCall     *geminiFnCall   `json:"functionCall,omitempty"`
	FunctionResponse *geminiFnResult `json:"functionResponse,omitempty"`
}

type geminiFnResult struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiTurn struct {
	Role  string          `json:"role"`
	Parts json.RawMessage `json:"parts"`
}

// geminiContents converts the neutral history to Gemini "contents".
func geminiContents(msgs []ChatMessage, model string) []geminiTurn {
	var out []geminiTurn
	var pendingResults []geminiChatPart
	flush := func() {
		if len(pendingResults) > 0 {
			raw, _ := json.Marshal(pendingResults)
			out = append(out, geminiTurn{Role: "user", Parts: raw})
			pendingResults = nil
		}
	}
	for _, m := range msgs {
		switch m.Role {
		case "tool":
			pendingResults = append(pendingResults, geminiChatPart{FunctionResponse: &geminiFnResult{
				ID: m.ToolCallID, Name: m.ToolName, Response: map[string]any{"result": m.Content},
			}})
		case "assistant":
			flush()
			if len(m.geminiParts) > 0 && m.geminiModel == model {
				out = append(out, geminiTurn{Role: "model", Parts: m.geminiParts})
				continue
			}
			var parts []geminiChatPart
			if m.Content != "" {
				parts = append(parts, geminiChatPart{Text: m.Content})
			}
			for i, tc := range m.ToolCalls {
				p := geminiChatPart{FunctionCall: &geminiFnCall{ID: tc.ID, Name: tc.Name, Args: nonEmptyJSON(tc.Args)}}
				if i == 0 {
					p.ThoughtSignature = geminiDummySignature
				}
				parts = append(parts, p)
			}
			raw, _ := json.Marshal(parts)
			out = append(out, geminiTurn{Role: "model", Parts: raw})
		default:
			flush()
			raw, _ := json.Marshal([]geminiChatPart{{Text: m.Content}})
			out = append(out, geminiTurn{Role: "user", Parts: raw})
		}
	}
	flush()
	return out
}

func nonEmptyJSON(raw json.RawMessage) json.RawMessage {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func (g *Gateway) chatGemini(ctx context.Context, model, system string, msgs []ChatMessage, tools []ToolSpec, maxTokens int) (ChatMessage, int, error) {
	decls := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		decls = append(decls, map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters})
	}
	body := map[string]any{
		"systemInstruction": map[string]any{"parts": []map[string]string{{"text": system}}},
		"contents":          geminiContents(msgs, model),
		"generationConfig":  map[string]any{"maxOutputTokens": maxTokens + 6000, "temperature": 0.3},
	}
	if len(decls) > 0 {
		body["tools"] = []map[string]any{{"functionDeclarations": decls}}
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts json.RawMessage `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			TotalTokenCount int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)
	if err := g.post(ctx, url, map[string]string{"x-goog-api-key": g.cfg.GeminiAPIKey}, body, &out); err != nil {
		return ChatMessage{}, 0, fmt.Errorf("gemini %s: %w", model, err)
	}
	if len(out.Candidates) == 0 {
		return ChatMessage{}, out.UsageMetadata.TotalTokenCount, fmt.Errorf("gemini %s: no candidates", model)
	}
	var parts []geminiChatPart
	if err := json.Unmarshal(out.Candidates[0].Content.Parts, &parts); err != nil {
		return ChatMessage{}, out.UsageMetadata.TotalTokenCount, fmt.Errorf("gemini %s: bad parts: %w", model, err)
	}
	msg := ChatMessage{Role: "assistant", geminiParts: out.Candidates[0].Content.Parts, geminiModel: model}
	var text strings.Builder
	for i, p := range parts {
		switch {
		case p.FunctionCall != nil:
			id := p.FunctionCall.ID
			if id == "" {
				id = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), i)
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: id, Name: p.FunctionCall.Name, Args: nonEmptyJSON(p.FunctionCall.Args)})
		case p.Text != "" && !p.Thought:
			text.WriteString(p.Text)
		}
	}
	msg.Content = text.String()
	if msg.Content == "" && len(msg.ToolCalls) == 0 {
		return msg, out.UsageMetadata.TotalTokenCount, fmt.Errorf("gemini %s: no text or tool call (finish %s)", model, out.Candidates[0].FinishReason)
	}
	return msg, out.UsageMetadata.TotalTokenCount, nil
}

// ---------------- OpenAI format (Groq) ----------------

func openAIMessages(system string, msgs []ChatMessage) []map[string]any {
	out := []map[string]any{{"role": "system", "content": system}}
	for _, m := range msgs {
		switch m.Role {
		case "assistant":
			om := map[string]any{"role": "assistant", "content": m.Content}
			if len(m.ToolCalls) > 0 {
				calls := make([]map[string]any, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					calls = append(calls, map[string]any{"id": tc.ID, "type": "function",
						"function": map[string]any{"name": tc.Name, "arguments": string(nonEmptyJSON(tc.Args))}})
				}
				om["tool_calls"] = calls
			}
			out = append(out, om)
		case "tool":
			out = append(out, map[string]any{"role": "tool", "tool_call_id": m.ToolCallID, "content": m.Content})
		default:
			out = append(out, map[string]any{"role": "user", "content": m.Content})
		}
	}
	return out
}

func openAITools(tools []ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{"type": "function", "function": map[string]any{
			"name": t.Name, "description": t.Description, "parameters": t.Parameters}})
	}
	return out
}

func (g *Gateway) chatOpenAI(ctx context.Context, url, key, model, system string, msgs []ChatMessage, tools []ToolSpec, maxTokens int) (ChatMessage, int, error) {
	body := map[string]any{
		"model": model, "temperature": 0.3, "max_tokens": maxTokens,
		"messages": openAIMessages(system, msgs),
	}
	if len(tools) > 0 {
		body["tools"] = openAITools(tools)
		body["tool_choice"] = "auto"
	}
	if strings.HasPrefix(model, "openai/gpt-oss") {
		body["reasoning_effort"] = "low"
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := g.post(ctx, url, map[string]string{"Authorization": "Bearer " + key}, body, &out); err != nil {
		return ChatMessage{}, 0, fmt.Errorf("groq %s: %w", model, err)
	}
	if len(out.Choices) == 0 {
		return ChatMessage{}, out.Usage.TotalTokens, fmt.Errorf("groq %s: no choices", model)
	}
	c := out.Choices[0].Message
	msg := ChatMessage{Role: "assistant", Content: c.Content}
	for _, tc := range c.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: nonEmptyJSON(json.RawMessage(tc.Function.Arguments))})
	}
	return msg, out.Usage.TotalTokens, nil
}

// ---------------- Cohere ----------------

func (g *Gateway) chatCohere(ctx context.Context, model, system string, msgs []ChatMessage, tools []ToolSpec, maxTokens int) (ChatMessage, int, error) {
	cm := []map[string]any{{"role": "system", "content": system}}
	for _, m := range msgs {
		switch m.Role {
		case "assistant":
			om := map[string]any{"role": "assistant"}
			if len(m.ToolCalls) > 0 {
				calls := make([]map[string]any, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					calls = append(calls, map[string]any{"id": tc.ID, "type": "function",
						"function": map[string]any{"name": tc.Name, "arguments": string(nonEmptyJSON(tc.Args))}})
				}
				om["tool_calls"] = calls
				if m.Content != "" {
					om["tool_plan"] = m.Content
				}
			} else {
				om["content"] = m.Content
			}
			cm = append(cm, om)
		case "tool":
			cm = append(cm, map[string]any{"role": "tool", "tool_call_id": m.ToolCallID, "content": m.Content})
		default:
			cm = append(cm, map[string]any{"role": "user", "content": m.Content})
		}
	}
	body := map[string]any{"model": model, "max_tokens": maxTokens, "temperature": 0.3, "messages": cm}
	if len(tools) > 0 {
		body["tools"] = openAITools(tools)
	}
	var out struct {
		Message struct {
			ToolPlan string `json:"tool_plan"`
			Content  []struct {
				Text string `json:"text"`
			} `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		Usage struct {
			Tokens struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"tokens"`
		} `json:"usage"`
	}
	if err := g.post(ctx, "https://api.cohere.com/v2/chat", map[string]string{"Authorization": "Bearer " + g.cfg.CohereAPIKey}, body, &out); err != nil {
		return ChatMessage{}, 0, fmt.Errorf("cohere %s: %w", model, err)
	}
	msg := ChatMessage{Role: "assistant", Content: out.Message.ToolPlan}
	for _, c := range out.Message.Content {
		msg.Content += c.Text
	}
	for _, tc := range out.Message.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: nonEmptyJSON(json.RawMessage(tc.Function.Arguments))})
	}
	return msg, out.Usage.Tokens.InputTokens + out.Usage.Tokens.OutputTokens, nil
}
