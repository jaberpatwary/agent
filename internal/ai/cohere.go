package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const cohereChatEndpoint = "https://api.cohere.com/v2/chat"

type CohereClient struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewCohereClient(apiKey, model string) *CohereClient {
	return &CohereClient{apiKey: apiKey, model: model, http: &http.Client{Timeout: 45 * time.Second}}
}

func (c *CohereClient) Name() string  { return "cohere" }
func (c *CohereClient) Model() string { return c.model }

func (c *CohereClient) Summarize(ctx context.Context, title, content string) (string, int, error) {
	return c.Complete(ctx, bangaEditorSystemPrompt, buildSummaryPrompt(title, content), 300)
}

func (c *CohereClient) Complete(ctx context.Context, systemPrompt, userPrompt string, maxTokens int) (string, int, error) {
	if c.apiKey == "" {
		return "", 0, fmt.Errorf("cohere: COHERE_API_KEY is not set")
	}
	requestBody := struct {
		Model     string        `json:"model"`
		Messages  []groqMessage `json:"messages"`
		MaxTokens int           `json:"max_tokens"`
	}{
		Model: c.model,
		Messages: []groqMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens: maxTokens,
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return "", 0, fmt.Errorf("cohere: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cohereChatEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("cohere: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("cohere: request failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", 0, fmt.Errorf("cohere: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", 0, fmt.Errorf("cohere: http %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var parsed struct {
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
		Meta struct {
			Tokens struct {
				Input  int `json:"input_tokens"`
				Output int `json:"output_tokens"`
			} `json:"tokens"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return "", 0, fmt.Errorf("cohere: decode response: %w", err)
	}
	var text strings.Builder
	for _, part := range parsed.Message.Content {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", parsed.Meta.Tokens.Input + parsed.Meta.Tokens.Output, fmt.Errorf("cohere: empty response")
	}
	return text.String(), parsed.Meta.Tokens.Input + parsed.Meta.Tokens.Output, nil
}
