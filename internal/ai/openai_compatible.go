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

// CompatibleClient calls providers that expose the OpenAI chat-completions API.
type CompatibleClient struct {
	name     string
	endpoint string
	apiKey   string
	model    string
	http     *http.Client
}

func NewCompatibleClient(name, endpoint, apiKey, model string) *CompatibleClient {
	return &CompatibleClient{
		name: name, endpoint: endpoint, apiKey: apiKey, model: model,
		http: &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *CompatibleClient) Name() string  { return c.name }
func (c *CompatibleClient) Model() string { return c.model }

func (c *CompatibleClient) Summarize(ctx context.Context, title, content string) (string, int, error) {
	return c.Complete(ctx, bangaEditorSystemPrompt, buildSummaryPrompt(title, content), 300)
}

func (c *CompatibleClient) Complete(ctx context.Context, systemPrompt, userPrompt string, maxTokens int) (string, int, error) {
	if c.apiKey == "" {
		return "", 0, fmt.Errorf("%s: API key is not set", c.name)
	}
	body, err := json.Marshal(groqRequest{
		Model: c.model,
		Messages: []groqMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens: maxTokens,
	})
	if err != nil {
		return "", 0, fmt.Errorf("%s: marshal request: %w", c.name, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("%s: build request: %w", c.name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("%s: request failed: %w", c.name, err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", 0, fmt.Errorf("%s: read response: %w", c.name, err)
	}
	if resp.StatusCode >= 400 {
		return "", 0, fmt.Errorf("%s: http %d: %s", c.name, resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var parsed groqResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return "", 0, fmt.Errorf("%s: decode response: %w", c.name, err)
	}
	if parsed.Error != nil {
		return "", 0, fmt.Errorf("%s: API error: %s", c.name, parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", parsed.Usage.TotalTokens, fmt.Errorf("%s: empty response", c.name)
	}
	return parsed.Choices[0].Message.Content, parsed.Usage.TotalTokens, nil
}
