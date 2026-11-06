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
