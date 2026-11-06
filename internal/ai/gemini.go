package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const geminiEndpointTemplate = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"

// GeminiClient calls Google's Gemini generateContent endpoint.
type GeminiClient struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewGeminiClient(apiKey, model string) *GeminiClient {
	return &GeminiClient{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 45 * time.Second},
	}
}

func (g *GeminiClient) Name() string  { return "gemini" }
func (g *GeminiClient) Model() string { return g.model }

type geminiRequest struct {
	Contents          []geminiContent  `json:"contents"`
	SystemInstruction *geminiContent   `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		TotalTokenCount int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (g *GeminiClient) Summarize(ctx context.Context, title, content string) (string, int, error) {
	return g.Complete(ctx, bangaEditorSystemPrompt, buildSummaryPrompt(title, content), 300)
}

func (g *GeminiClient) Complete(ctx context.Context, systemPrompt, userPrompt string, maxTokens int) (string, int, error) {
	if g.apiKey == "" {
		return "", 0, fmt.Errorf("gemini: GEMINI_API_KEY is not set")
	}

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: userPrompt}}},
		},
	}
	if systemPrompt != "" {
		reqBody.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: systemPrompt}}}
	}
	if maxTokens > 0 {
		reqBody.GenerationConfig = &geminiGenConfig{MaxOutputTokens: maxTokens}
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", 0, fmt.Errorf("gemini: marshal request: %w", err)
	}

	endpoint := fmt.Sprintf(geminiEndpointTemplate, url.PathEscape(g.model))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", 0, fmt.Errorf("gemini: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.apiKey)

	resp, err := g.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("gemini: request failed: %s", redactGeminiKey(err.Error(), g.apiKey))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("gemini: read response: %s", redactGeminiKey(err.Error(), g.apiKey))
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", 0, fmt.Errorf("gemini: rate limited (429)")
	}
	if resp.StatusCode >= 400 {
		return "", 0, fmt.Errorf("gemini: http %d: %s", resp.StatusCode, redactGeminiKey(string(body), g.apiKey))
	}

	var parsed geminiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", 0, fmt.Errorf("gemini: unmarshal response: %w", err)
	}
	if parsed.Error != nil {
		return "", 0, fmt.Errorf("gemini: api error: %s", redactGeminiKey(parsed.Error.Message, g.apiKey))
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", 0, fmt.Errorf("gemini: empty candidates in response")
	}

	return parsed.Candidates[0].Content.Parts[0].Text, parsed.UsageMetadata.TotalTokenCount, nil
}

func redactGeminiKey(value, apiKey string) string {
	if apiKey == "" {
		return value
	}
	return strings.ReplaceAll(value, apiKey, "[REDACTED]")
}
