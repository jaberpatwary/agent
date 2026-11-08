package ai

import (
	"errors"
	"testing"
	"time"

	"bangla-news-aggregator/internal/config"
)

func testGatewayConfig() *config.Config {
	return &config.Config{
		GroqAPIKey: "g", GeminiAPIKey: "m", CohereAPIKey: "c",
		GeminiModel: "gem-a", GeminiFallbackModel: "gem-b", CohereModel: "cmd",
		GatewayGroqWriteModel: "big", GatewayGroqCheckModel: "small",
		CloudflareAPIToken: "t", CloudflareModel: "cf", // no account id -> Cloudflare skipped
	}
}

func routeNames(rs []gatewayRoute) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.provider + "/" + r.model
	}
	return out
}

func TestGatewayRoutes(t *testing.T) {
	g := NewGateway(testGatewayConfig())
	cases := map[string]struct {
		req  GatewayRequest
		want []string
	}{
		"writing prefers Gemini, ends with Cohere":  {GatewayRequest{Task: "rewrite"}, []string{"gemini/gem-a", "gemini/gem-b", "groq/big", "cohere/cmd"}},
		"fake check searches with the big model":    {GatewayRequest{Task: "fake_check", Search: true}, []string{"groq/big", "groq/small", "gemini/gem-a"}},
		"cross check searches with the small model": {GatewayRequest{Task: "cross_check", Search: true}, []string{"groq/small", "groq/big", "gemini/gem-a"}},
	}
	for name, tc := range cases {
		got := routeNames(g.routes(tc.req))
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %v, want %v", name, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: got %v, want %v", name, got, tc.want)
			}
		}
	}
}

func TestGatewayCooldownSkipsBusyProvider(t *testing.T) {
	g := NewGateway(testGatewayConfig())
	g.coolDown("gemini/gem-a", errors.New("gemini gem-a: http 503: high demand"))
	if !g.coolingDown("gemini/gem-a") {
		t.Fatal("a 503 should park the model for a while")
	}
	if g.coolingDown("gemini/gem-b") {
		t.Fatal("other models must stay available")
	}
}

func TestGatewayCooldownUsesProviderRetryHint(t *testing.T) {
	g := NewGateway(testGatewayConfig())
	g.coolDown("groq/big", errors.New("groq big: http 429: Rate limit reached ... on tokens per day (TPD): Limit 200000. Please try again in 5m55.968s."))
	until := g.cooldown["groq/big"]
	if left := time.Until(until); left < 5*time.Minute || left > 7*time.Minute {
		t.Fatalf("expected ~6m cooldown from the retry hint, got %s", left)
	}
}

// A used-up daily quota names its reset time; the model is skipped until then.
func TestDailyQuotaCooldownFollowsTheProvidersRetryDelay(t *testing.T) {
	g := &Gateway{cooldown: map[string]time.Time{}}
	g.coolDown("gemini/m", errors.New(`gemini m: http 429 (retry in 9215s): { "error": { "code": 429, "message": "You exceeded your current quota"`))
	left := time.Until(g.cooldown["gemini/m"])
	if left < 2*time.Hour || left > 3*time.Hour {
		t.Fatalf("expected about 2.5h of cooldown, got %v", left)
	}
	if m := retryDelayRegex.FindSubmatch([]byte(`"details": [{"retryDelay": "9215s"}]`)); m == nil || string(m[1]) != "9215" {
		t.Fatalf("retryDelay not found: %q", m)
	}
}

// A better model that is only rate-limited for a minute is worth waiting for
// before spending the last resort's (Cohere's) small monthly allowance.
func TestShortRateLimitIsWaitedOutBeforeTheLastResort(t *testing.T) {
	g := &Gateway{cooldown: map[string]time.Time{}}
	routes := []gatewayRoute{{provider: "gemini", model: "a"}, {provider: "gemini", model: "b"}, {provider: "cohere", model: "c"}}
	g.cooldown["gemini/a"] = time.Now().Add(40 * time.Second)
	g.cooldown["gemini/b"] = time.Now().Add(3 * time.Hour) // daily quota: not worth waiting for
	if w := g.shortWait(routes); w <= 30*time.Second || w > 40*time.Second {
		t.Fatalf("expected to wait ~40s for model a, got %v", w)
	}
	// Two briefly limited models: wait until both are free, not just the first.
	g.cooldown["gemini/b"] = time.Now().Add(20 * time.Second)
	if w := g.shortWait(routes); w <= 30*time.Second {
		t.Fatalf("expected to wait for the later of the two (~40s), got %v", w)
	}
	// A request too large for one model says nothing about the model's quota.
	g.coolDown("groq/q", errors.New("groq q: http 413: Request too large"))
	if g.coolingDown("groq/q") {
		t.Fatal("413 must not park the model")
	}
	g.cooldown["gemini/a"] = time.Now().Add(2 * time.Hour)
	g.cooldown["gemini/b"] = time.Now().Add(2 * time.Hour)
	if w := g.shortWait(routes); w != 0 {
		t.Fatalf("nothing is free soon, so no waiting: got %v", w)
	}
}
