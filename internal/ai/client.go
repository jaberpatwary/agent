package ai

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"bangla-news-aggregator/internal/config"
)

// Result carries the outcome of a single provider call (a summary call or a
// generic Complete call), including metadata used for ai_usage logging
// regardless of success or failure.
type Result struct {
	Provider   string
	Model      string
	Text       string
	TokensUsed int
	Duration   time.Duration
	Err        error
}

// Summarizer is implemented by each AI provider (Groq, Gemini, ...). Complete
// is the generic chat-completion primitive every provider exposes; Summarize
// is kept as a convenience wrapper so existing callers are unaffected.
type Summarizer interface {
	Name() string
	Model() string
	Summarize(ctx context.Context, title, content string) (summary string, tokensUsed int, err error)
	Complete(ctx context.Context, systemPrompt, userPrompt string, maxTokens int) (text string, tokensUsed int, err error)
}

// Client tries configured providers in order and falls through on errors.
type Client struct {
	providers []Summarizer
	statesMu  sync.Mutex
	states    map[string]*providerState
}

type providerState struct {
	gate          chan struct{}
	cooldownUntil time.Time
}

// NewClient builds the fallback-aware AI client from configuration.
func NewClient(cfg *config.Config) *Client {
	providers := make([]Summarizer, 0, 5)
	if cfg.OpenRouterAPIKey != "" {
		providers = append(providers, NewCompatibleClient("openrouter", "https://openrouter.ai/api/v1/chat/completions", cfg.OpenRouterAPIKey, cfg.OpenRouterModel))
	}
	if cfg.GeminiAPIKey != "" {
		providers = append(providers, NewGeminiClient(cfg.GeminiAPIKey, cfg.GeminiModel))
	}
	if cfg.GroqAPIKey != "" {
		providers = append(providers, NewGroqClient(cfg.GroqAPIKey, cfg.GroqModel))
	}
	if cfg.SambaNovaAPIKey != "" {
		providers = append(providers, NewCompatibleClient("sambanova", "https://api.sambanova.ai/v1/chat/completions", cfg.SambaNovaAPIKey, cfg.SambaNovaModel))
	}
	if cfg.CohereAPIKey != "" {
		providers = append(providers, NewCohereClient(cfg.CohereAPIKey, cfg.CohereModel))
	}
	return &Client{providers: providers, states: make(map[string]*providerState)}
}

// HasProviders reports whether at least one AI provider is configured.
func (c *Client) HasProviders() bool { return len(c.providers) > 0 }

// Summarize tries each configured provider in order. All attempted results
// are returned so successes and failures can be logged for usage tracking.
func (c *Client) Summarize(ctx context.Context, title, content string) (Result, []Result) {
	return c.run(ctx, func(p Summarizer) (string, int, error) {
		return p.Summarize(ctx, title, content)
	})
}

// Complete runs a free-form system+user prompt through the same
// fallback/circuit-breaker chain as Summarize. Used by the agentic rewriter
// for the draft and self-review steps, where the output is expected to be a
// larger, structured (JSON) response rather than a short summary.
func (c *Client) Complete(ctx context.Context, systemPrompt, userPrompt string, maxTokens int) (Result, []Result) {
	return c.run(ctx, func(p Summarizer) (string, int, error) {
		return p.Complete(ctx, systemPrompt, userPrompt, maxTokens)
	})
}

// run drives the shared fallback loop: acquire the provider's single-flight
// gate, skip providers in cooldown, call them, and record a cooldown on
// failure so a provider that is rate-limited or unauthorized is not retried
// on every single article.
func (c *Client) run(ctx context.Context, call func(Summarizer) (string, int, error)) (Result, []Result) {
	var attempts []Result
	for i, provider := range c.providers {
		state := c.stateFor(provider.Name())
		select {
		case state.gate <- struct{}{}:
		case <-ctx.Done():
			break
		}
		if ctx.Err() != nil {
			select {
			case <-state.gate:
			default:
			}
			break
		}
		if wait := time.Until(state.cooldownUntil); wait > 0 {
			<-state.gate
			continue
		}

		start := time.Now()
		text, tokens, err := call(provider)
		if err == nil && strings.TrimSpace(text) == "" {
			err = errors.New("provider returned an empty response")
		}
		if err != nil {
			if cooldown := providerCooldown(err); cooldown > 0 {
				state.cooldownUntil = time.Now().Add(cooldown)
				log.Printf("[ai] provider %q cooling down for %s after failure: %v", provider.Name(), cooldown, err)
			}
		}
		<-state.gate

		result := Result{
			Provider: provider.Name(), Model: provider.Model(), Text: text,
			TokensUsed: tokens, Duration: time.Since(start), Err: err,
		}
		attempts = append(attempts, result)
		if err == nil && strings.TrimSpace(text) != "" {
			return result, attempts
		}
		if ctx.Err() != nil {
			break
		}
		if i+1 < len(c.providers) {
			log.Printf("[ai] provider %q failed (%v), falling back to %q", provider.Name(), result.Err, c.providers[i+1].Name())
		}
	}
	return Result{Err: errors.New("all configured AI providers failed")}, attempts
}

func (c *Client) stateFor(name string) *providerState {
	c.statesMu.Lock()
	defer c.statesMu.Unlock()
	if c.states == nil {
		c.states = make(map[string]*providerState)
	}
	state := c.states[name]
	if state == nil {
		state = &providerState{gate: make(chan struct{}, 1)}
		c.states[name] = state
	}
	return state
}

func providerCooldown(err error) time.Duration {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "http 401"), strings.Contains(message, "http 403"):
		return 6 * time.Hour
	case strings.Contains(message, "http 402"):
		return time.Hour
	case strings.Contains(message, "openrouter") && strings.Contains(message, "429"):
		return time.Hour
	case strings.Contains(message, "429"), strings.Contains(message, "rate limit"):
		return 3 * time.Minute
	case strings.Contains(message, "http 503"), strings.Contains(message, "deadline exceeded"):
		return time.Minute
	case strings.Contains(message, "empty response"), strings.Contains(message, "empty candidates"):
		return 30 * time.Second
	default:
		return 15 * time.Second
	}
}
