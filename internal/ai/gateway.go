package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"bangla-news-aggregator/internal/config"
)

// Gateway is the single AI entry point for the n8n pipeline
// (POST /api/n8n/ai). n8n says *what* it needs (a task, prompts, JSON or
// web search) and the gateway decides *who* answers, trying providers in a
// task-specific order and skipping any that are rate-limited or overloaded.
//
//	fake_check, cross_check (need web search) -> Groq models with browser_search, then Gemini with Google Search
//	rewrite, review (Bangla writing)          -> Gemini, Gemini fallback model, Groq, Cohere, Cloudflare
type Gateway struct {
	cfg  *config.Config
	http *http.Client

	mu       sync.Mutex
	cooldown map[string]time.Time // "provider/model" -> skip until
}

// GatewayRequest is what n8n sends.
type GatewayRequest struct {
	Task      string `json:"task"` // fake_check | cross_check | rewrite | review
	System    string `json:"system"`
	User      string `json:"user"`
	MaxTokens int    `json:"max_tokens"`
	JSON      bool   `json:"json"`   // ask for a JSON object (ignored when Search is on: providers can't combine the two)
	Search    bool   `json:"search"` // the model must be able to search the web
}

type gatewayRoute struct {
	provider string // groq | gemini | cohere | cloudflare
	model    string
	search   bool
}

func NewGateway(cfg *config.Config) *Gateway {
	// Long Bangla prompts can take Cohere and Gemma well over a minute.
	return &Gateway{cfg: cfg, http: &http.Client{Timeout: 115 * time.Second}, cooldown: map[string]time.Time{}}
}

func (g *Gateway) routes(req GatewayRequest) []gatewayRoute {
	c := g.cfg
	var out []gatewayRoute
	add := func(provider, key, model string, search bool) {
		if key != "" && model != "" {
			out = append(out, gatewayRoute{provider: provider, model: model, search: search})
		}
	}
	if req.Search {
		first, second := c.GatewayGroqWriteModel, c.GatewayGroqCheckModel
		if req.Task == "cross_check" {
			first, second = second, first // the heavy multi-page search goes to the cheaper model first
		}
		add("groq", c.GroqAPIKey, first, true)
		add("groq", c.GroqAPIKey, second, true)
		add("gemini", c.GeminiAPIKey, c.GeminiModel, true)
		return out
	}
	for _, m := range c.GeminiChain() {
		add("gemini", c.GeminiAPIKey, m, false)
	}
	for _, m := range c.GroqChain() {
		add("groq", c.GroqAPIKey, m, false)
	}
	add("cohere", c.CohereAPIKey, c.CohereModel, false)
	if c.CloudflareAccountID != "" {
		add("cloudflare", c.CloudflareAPIToken, c.CloudflareModel, false)
	}
	return out
}

// Run tries each route in order and returns the first success plus every attempt (for usage logging).
func (g *Gateway) Run(ctx context.Context, req GatewayRequest) (Result, []Result) {
	if req.MaxTokens <= 0 {
		req.MaxTokens = 3000
	}
	var attempts []Result
	routes := g.routes(req)
	waited := false
	for i := 0; i < len(routes); i++ {
		rt := routes[i]
		if !waited && rt.provider == lastResortProvider {
			if w := g.shortWait(routes); w > 0 {
				waited = true
				if sleepCtx(ctx, w) {
					i = -1 // start again from the best model
					continue
				}
			}
		}
		key := rt.provider + "/" + rt.model
		if g.coolingDown(key) {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		start := time.Now()
		text, tokens, err := g.call(ctx, rt, req)
		if err == nil && strings.TrimSpace(text) == "" {
			err = errors.New("empty response")
		}
		res := Result{Provider: rt.provider, Model: rt.model, Text: text, TokensUsed: tokens, Duration: time.Since(start), Err: err}
		attempts = append(attempts, res)
		if err == nil {
			return res, attempts
		}
		g.coolDown(key, err)
	}
	if len(attempts) == 0 {
		return Result{Err: errors.New("no AI provider available (all cooling down or not configured)")}, attempts
	}
	return Result{Err: fmt.Errorf("all AI providers failed; last: %v", attempts[len(attempts)-1].Err)}, attempts
}

func (g *Gateway) coolingDown(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return time.Now().Before(g.cooldown[key])
}

// Cohere's free key has a small monthly allowance, so it is the last resort.
const (
	lastResortProvider = "cohere"
	maxShortWait       = 75 * time.Second
)

// shortWait returns how long until every better model that is only briefly
// rate-limited (a per-minute quota) is free again, or 0 if none is. Waiting
// a minute for them beats spending the last resort's scarce, slow calls.
func (g *Gateway) shortWait(routes []gatewayRoute) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	longest := time.Duration(0)
	for _, rt := range routes {
		if rt.provider == lastResortProvider {
			continue
		}
		left := time.Until(g.cooldown[rt.provider+"/"+rt.model])
		if left > 0 && left <= maxShortWait && left > longest {
			longest = left
		}
	}
	return longest
}

// sleepCtx waits for d; it returns false if ctx ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// coolDown parks a failing provider/model so the next stories skip it instead
// of burning a slow failed call each time.
func (g *Gateway) coolDown(key string, err error) {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "http 413") {
		return // this request was too big for the model; smaller ones still fit
	}
	d := 30 * time.Second
	switch {
	case strings.Contains(msg, "tokens per day"), strings.Contains(msg, "(tpd)"), strings.Contains(msg, "quota"):
		d = time.Hour
	case strings.Contains(msg, "http 429"):
		d = 3 * time.Minute
	case strings.Contains(msg, "http 503"), strings.Contains(msg, "high demand"), strings.Contains(msg, "overloaded"):
		d = 2 * time.Minute
	case strings.Contains(msg, "http 401"), strings.Contains(msg, "http 403"):
		d = 6 * time.Hour
	}
	// Providers often say exactly when capacity returns ("try again in 5m55.9s");
	// trust that over the generic guess, within sane bounds.
	if m := retryAfterRegex.FindStringSubmatch(msg); m != nil {
		if hint, perr := time.ParseDuration(m[1]); perr == nil && hint > 0 {
			d = min(max(hint+5*time.Second, 20*time.Second), 2*time.Hour)
		}
	}
	// A used-up daily quota names the reset time; skip the model until then
	// instead of spending a failed call on it every hour.
	if m := retryInRegex.FindStringSubmatch(msg); m != nil {
		if secs, perr := time.ParseDuration(m[1] + "s"); perr == nil && secs > 0 {
			d = min(max(secs+10*time.Second, 20*time.Second), 20*time.Hour)
		}
	}
	g.mu.Lock()
	g.cooldown[key] = time.Now().Add(d)
	g.mu.Unlock()
}

var (
	retryAfterRegex = regexp.MustCompile(`try again in ((?:\d+h)?(?:\d+m)?(?:[\d.]+s)?)`)
	retryInRegex    = regexp.MustCompile(`\(retry in ([\d.]+)s\)`)
	retryDelayRegex = regexp.MustCompile(`"retryDelay":\s*"([\d.]+)s"`)
)

func (g *Gateway) call(ctx context.Context, rt gatewayRoute, req GatewayRequest) (string, int, error) {
	switch rt.provider {
	case "groq":
		return g.callOpenAI(ctx, "https://api.groq.com/openai/v1/chat/completions", g.cfg.GroqAPIKey, rt, req)
	case "cloudflare":
		url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/v1/chat/completions", g.cfg.CloudflareAccountID)
		return g.callOpenAI(ctx, url, g.cfg.CloudflareAPIToken, rt, req)
	case "gemini":
		return g.callGemini(ctx, rt, req)
	case "cohere":
		return g.callCohere(ctx, rt, req)
	}
	return "", 0, fmt.Errorf("unknown provider %q", rt.provider)
}

func (g *Gateway) post(ctx context.Context, url string, headers map[string]string, body any, out any) error {
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := g.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		// Gemini says when a used-up quota returns ("retryDelay": "9215s"),
		// deep inside a long body; keep it so coolDown can honour it.
		hint := ""
		if m := retryDelayRegex.FindSubmatch(data); m != nil {
			hint = " (retry in " + string(m[1]) + "s)"
		}
		return fmt.Errorf("http %d%s: %s", resp.StatusCode, hint, truncateForError(string(data)))
	}
	return json.Unmarshal(data, out)
}

func truncateForError(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

// callOpenAI covers Groq and Cloudflare (both OpenAI-compatible).
func (g *Gateway) callOpenAI(ctx context.Context, url, key string, rt gatewayRoute, req GatewayRequest) (string, int, error) {
	body := map[string]any{
		"model":       rt.model,
		"temperature": 0.3,
		"max_tokens":  req.MaxTokens,
		"messages": []map[string]string{
			{"role": "system", "content": req.System},
			{"role": "user", "content": req.User},
		},
	}
	if strings.HasPrefix(rt.model, "openai/gpt-oss") {
		body["reasoning_effort"] = "low"
	}
	if rt.search {
		body["tools"] = []map[string]string{{"type": "browser_search"}}
	} else if req.JSON {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := g.post(ctx, url, map[string]string{"Authorization": "Bearer " + key}, body, &out); err != nil {
		return "", 0, fmt.Errorf("%s %s: %w", rt.provider, rt.model, err)
	}
	if len(out.Choices) == 0 {
		return "", out.Usage.TotalTokens, fmt.Errorf("%s %s: no choices", rt.provider, rt.model)
	}
	return out.Choices[0].Message.Content, out.Usage.TotalTokens, nil
}

func (g *Gateway) callGemini(ctx context.Context, rt gatewayRoute, req GatewayRequest) (string, int, error) {
	gen := map[string]any{
		// Newer Gemini models "think" before answering and that counts against
		// this budget, so give it generous headroom over the visible answer.
		"maxOutputTokens": req.MaxTokens + 6000,
		"temperature":     0.3,
	}
	body := map[string]any{
		"systemInstruction": map[string]any{"parts": []map[string]string{{"text": req.System}}},
		"contents":          []map[string]any{{"role": "user", "parts": []map[string]string{{"text": req.User}}}},
		"generationConfig":  gen,
	}
	if rt.search {
		body["tools"] = []map[string]any{{"google_search": map[string]any{}}}
	} else if req.JSON {
		gen["responseMimeType"] = "application/json"
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			TotalTokenCount int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", rt.model)
	if err := g.post(ctx, url, map[string]string{"x-goog-api-key": g.cfg.GeminiAPIKey}, body, &out); err != nil {
		return "", 0, fmt.Errorf("gemini %s: %w", rt.model, err)
	}
	if len(out.Candidates) == 0 {
		return "", out.UsageMetadata.TotalTokenCount, fmt.Errorf("gemini %s: no candidates", rt.model)
	}
	var b strings.Builder
	for _, p := range out.Candidates[0].Content.Parts {
		if !p.Thought {
			b.WriteString(p.Text)
		}
	}
	if fr := out.Candidates[0].FinishReason; fr != "" && fr != "STOP" && b.Len() == 0 {
		return "", out.UsageMetadata.TotalTokenCount, fmt.Errorf("gemini %s: finish reason %s", rt.model, fr)
	}
	return b.String(), out.UsageMetadata.TotalTokenCount, nil
}

func (g *Gateway) callCohere(ctx context.Context, rt gatewayRoute, req GatewayRequest) (string, int, error) {
	body := map[string]any{
		"model":       rt.model,
		"max_tokens":  req.MaxTokens,
		"temperature": 0.3,
		"messages": []map[string]string{
			{"role": "system", "content": req.System},
			{"role": "user", "content": req.User},
		},
	}
	if req.JSON {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	var out struct {
		Message struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
		Usage struct {
			Tokens struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"tokens"`
		} `json:"usage"`
	}
	if err := g.post(ctx, "https://api.cohere.com/v2/chat", map[string]string{"Authorization": "Bearer " + g.cfg.CohereAPIKey}, body, &out); err != nil {
		return "", 0, fmt.Errorf("cohere %s: %w", rt.model, err)
	}
	var b strings.Builder
	for _, c := range out.Message.Content {
		b.WriteString(c.Text)
	}
	return b.String(), out.Usage.Tokens.InputTokens + out.Usage.Tokens.OutputTokens, nil
}
