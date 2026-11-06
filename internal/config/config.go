package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	Port               string
	DatabaseURL        string
	JWTSecret          string
	JWTExpiry          time.Duration
	GroqAPIKey         string
	GroqModel          string
	GeminiAPIKey       string
	GeminiModel        string
	OpenRouterAPIKey   string
	OpenRouterModel    string
	SambaNovaAPIKey    string
	SambaNovaModel     string
	CohereAPIKey       string
	CohereModel        string
	CronInterval       time.Duration
	FeedWorkerCount    int
	AppEnv             string
	AdminDefaultUser   string
	AdminDefaultPass   string
	PublicBaseURL      string // e.g. https://news.example.com — used for sitemap.xml, canonical links, and OpenGraph/JSON-LD absolute URLs
	ImageStorageDir    string // local directory to cache fetched hero images in; empty disables image caching (articles keep hotlinking the source)
	AIDailyTokenBudget int    // 0 = unlimited. Once today's ai_usage token total reaches this, the cron run stops making new AI calls until the next UTC day.
	N8NAPIKey          string // shared secret for the /api/n8n routes; empty disables them

	// AI gateway (POST /api/n8n/ai) — see internal/ai/gateway.go.
	GeminiFallbackModel   string // second Gemini model tried when the first is busy
	GatewayGroqWriteModel string // larger Groq model (fake check, writing fallback)
	GatewayGroqCheckModel string // cheaper Groq model (source cross-check)
	CloudflareAPIToken    string
	CloudflareAccountID   string // Workers AI is used only when this is set
	CloudflareModel       string
}

// Load reads a .env file (if present) and then environment variables,
// applying sensible defaults where a value is not set.
func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("[config] no .env file found, relying on real environment variables")
	}

	cfg := &Config{
		Port:               getEnv("PORT", "8080"),
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		JWTSecret:          getEnv("JWT_SECRET", "change-this-secret-in-production"),
		JWTExpiry:          getEnvDuration("JWT_EXPIRY", 24*time.Hour),
		GroqAPIKey:         getEnv("GROQ_API_KEY", ""),
		GroqModel:          getEnv("GROQ_MODEL", "openai/gpt-oss-20b"),
		GeminiAPIKey:       getEnv("GEMINI_API_KEY", ""),
		GeminiModel:        getEnv("GEMINI_MODEL", "gemini-3.8-flash"),
		OpenRouterAPIKey:   getEnv("OPENROUTER_API_KEY", ""),
		OpenRouterModel:    getEnv("OPENROUTER_MODEL", "openrouter/free"),
		SambaNovaAPIKey:    getEnv("SAMBANOVA_API_KEY", ""),
		SambaNovaModel:     getEnv("SAMBANOVA_MODEL", "Meta-Llama-3.3-70B-Instruct"),
		CohereAPIKey:       getEnv("COHERE_API_KEY", ""),
		CohereModel:        getEnv("COHERE_MODEL", "command-a-03-2025"),
		CronInterval:       getEnvDuration("CRON_INTERVAL", 1*time.Hour),
		FeedWorkerCount:    getEnvInt("FEED_WORKER_COUNT", 5),
		AppEnv:             getEnv("APP_ENV", "development"),
		AdminDefaultUser:   getEnv("ADMIN_DEFAULT_USER", "admin"),
		AdminDefaultPass:   getEnv("ADMIN_DEFAULT_PASS", "admin123"),
		PublicBaseURL:      strings.TrimRight(getEnv("PUBLIC_BASE_URL", ""), "/"),
		ImageStorageDir:    getEnv("IMAGE_STORAGE_DIR", "./data/images"),
		AIDailyTokenBudget: getEnvInt("AI_DAILY_TOKEN_BUDGET", 0),
		N8NAPIKey:          strings.TrimSpace(getEnv("N8N_API_KEY", "")),

		GeminiFallbackModel:   getEnv("GEMINI_FALLBACK_MODEL", "gemini-3.1-flash-lite"),
		GatewayGroqWriteModel: getEnv("N8N_GROQ_MODEL", "openai/gpt-oss-120b"),
		GatewayGroqCheckModel: getEnv("N8N_CHECK_MODEL", "openai/gpt-oss-20b"),
		CloudflareAPIToken:    getEnv("CLOUDFLARE_API_TOKEN", ""),
		CloudflareAccountID:   getEnv("CLOUDFLARE_ACCOUNT_ID", ""),
		CloudflareModel:       getEnv("CLOUDFLARE_MODEL", "@cf/openai/gpt-oss-120b"),
	}

	if cfg.N8NAPIKey != "" && len(cfg.N8NAPIKey) < 24 {
		log.Fatal("[config] N8N_API_KEY must be at least 24 characters (or empty to disable the n8n API)")
	}

	if cfg.DatabaseURL == "" {
		log.Fatal("[config] DATABASE_URL is required (Neon/Supabase Postgres connection string)")
	}
	if cfg.CronInterval <= 0 {
		log.Fatal("[config] CRON_INTERVAL must be a positive Go duration, e.g. 1h")
	}
	if cfg.FeedWorkerCount < 1 {
		cfg.FeedWorkerCount = 1
	}
	if cfg.JWTExpiry <= 0 {
		log.Fatal("[config] JWT_EXPIRY must be a positive Go duration, e.g. 24h")
	}
	if cfg.AppEnv == "production" {
		if len(cfg.JWTSecret) < 32 || cfg.JWTSecret == "change-this-secret-in-production" {
			log.Fatal("[config] production requires a unique JWT_SECRET of at least 32 characters")
		}
		if cfg.AdminDefaultPass == "admin123" || len(cfg.AdminDefaultPass) < 12 {
			log.Fatal("[config] production requires ADMIN_DEFAULT_PASS to be changed to at least 12 characters before first startup")
		}
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
