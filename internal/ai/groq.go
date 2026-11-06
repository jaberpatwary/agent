package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const groqEndpoint = "https://api.groq.com/openai/v1/chat/completions"

// GroqClient calls Groq's OpenAI-compatible chat completions endpoint.
type GroqClient struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewGroqClient(apiKey, model string) *GroqClient {
	return &GroqClient{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 45 * time.Second},
	}
}

func (g *GroqClient) Name() string  { return "groq" }
func (g *GroqClient) Model() string { return g.model }

type groqRequest struct {
	Model     string        `json:"model"`
	Messages  []groqMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
}

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqResponse struct {
	Choices []struct {
		Message groqMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (g *GroqClient) Summarize(ctx context.Context, title, content string) (string, int, error) {
	return g.Complete(ctx, bangaEditorSystemPrompt, buildSummaryPrompt(title, content), 300)
}

// Complete is the generic chat-completion primitive: any system+user prompt
// pair, with a caller-chosen max token budget (summaries need little room,
// a full article rewrite needs much more).
func (g *GroqClient) Complete(ctx context.Context, systemPrompt, userPrompt string, maxTokens int) (string, int, error) {
	if g.apiKey == "" {
		return "", 0, fmt.Errorf("groq: GROQ_API_KEY is not set")
	}

	reqBody := groqRequest{
		Model: g.model,
		Messages: []groqMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens: maxTokens,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", 0, fmt.Errorf("groq: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, groqEndpoint, bytes.NewReader(payload))
	if err != nil {
		return "", 0, fmt.Errorf("groq: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.apiKey)

	resp, err := g.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("groq: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("groq: read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", 0, fmt.Errorf("groq: rate limited (429)")
	}
	if resp.StatusCode >= 400 {
		return "", 0, fmt.Errorf("groq: http %d: %s", resp.StatusCode, string(body))
	}

	var parsed groqResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", 0, fmt.Errorf("groq: unmarshal response: %w", err)
	}
	if parsed.Error != nil {
		return "", 0, fmt.Errorf("groq: api error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", 0, fmt.Errorf("groq: empty choices in response")
	}

	return parsed.Choices[0].Message.Content, parsed.Usage.TotalTokens, nil
}
