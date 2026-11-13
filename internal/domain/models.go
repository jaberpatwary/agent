package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Article represents a single news article, either AI-summarized from an
// RSS feed or manually created by an admin.
type Article struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Title        string    `gorm:"type:text;not null" json:"title"`
	Content      string    `gorm:"type:text;not null" json:"content"`
	Summary      string    `gorm:"type:text" json:"summary"`
	SourceURL    string    `gorm:"type:text;uniqueIndex;not null" json:"source_url"`
	SourceName   string    `gorm:"type:varchar(255)" json:"source_name"`
	ImageURL     string    `gorm:"type:text" json:"image_url"`
	Category     string    `gorm:"type:varchar(100);index" json:"category"`
	Status       string    `gorm:"type:varchar(20);default:'published';index" json:"status"` // draft | published | pending_review | archived
	ReviewReason string    `gorm:"type:text" json:"review_reason,omitempty"`                 // why the agent held this back for human approval, if any
	Sources      string    `gorm:"type:text" json:"sources,omitempty"`                       // comma-separated publisher names when this is a multi-source synthesis
	Tags         string    `gorm:"type:text;index" json:"tags,omitempty"`                    // comma-separated AI-assigned topic tags, used for the "related articles" feature
	Views        int64     `gorm:"default:0;not null" json:"views"`                          // public page views, for "most read"
	PublishedAt  time.Time `gorm:"index" json:"published_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// BeforeCreate ensures every article has a UUID even on drivers where the
// default gen_random_uuid() extension might not be enabled.
func (a *Article) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.PublishedAt.IsZero() {
		a.PublishedAt = time.Now()
	}
	return nil
}

// FeedSource represents one RSS feed the cron worker pulls from.
type FeedSource struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name      string    `gorm:"type:varchar(255);not null" json:"name"`
	URL       string    `gorm:"type:text;uniqueIndex;not null" json:"url"`
	Category  string    `gorm:"type:varchar(100)" json:"category"`
	Active    bool      `gorm:"default:true" json:"active"`
	LastFetch time.Time `json:"last_fetch"`
	LastError string    `gorm:"type:text" json:"last_error"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (f *FeedSource) BeforeCreate(tx *gorm.DB) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	return nil
}

// AIUsage logs every call made to Groq/Gemini for cost & reliability tracking.
type AIUsage struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Provider   string     `gorm:"type:varchar(50);not null" json:"provider"` // groq | gemini
	Model      string     `gorm:"type:varchar(100)" json:"model"`
	ArticleID  *uuid.UUID `gorm:"type:uuid;index" json:"article_id"`
	TokensUsed int        `json:"tokens_used"`
	Success    bool       `json:"success"`
	ErrorMsg   string     `gorm:"type:text" json:"error_msg"`
	DurationMs int64      `json:"duration_ms"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (u *AIUsage) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}

// AdminUser represents a back-office user who can log into /admin.
type AdminUser struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Username     string    `gorm:"type:varchar(100);uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"type:text;not null" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (u *AdminUser) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}

// SeenSourceURL records every RSS item URL that has already been folded
// into some Article — including the non-primary URLs of a multi-source
// synthesis, which don't otherwise appear anywhere in the articles table.
// Without this, the same real-world story covered by a second publisher
// would never be recognized as "already covered" in a later cron run.
type SeenSourceURL struct {
	SourceURL string    `gorm:"type:text;primaryKey" json:"source_url"`
	ArticleID uuid.UUID `gorm:"type:uuid;index" json:"article_id"`
	CreatedAt time.Time `json:"created_at"`
}

// AppSetting is a generic key/value row for runtime-editable configuration
// (currently just the AI rewrite house style) that admins can change from
// the UI without a redeploy.
type AppSetting struct {
	Key       string    `gorm:"type:varchar(100);primaryKey" json:"key"`
	Value     string    `gorm:"type:text;not null" json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewsJourney tracks one story's trip through the external (n8n) pipeline —
// received → fake check → source cross-check → rewrite → published/rejected —
// as a single row keyed by NewsID, so an admin can see the whole history of
// a story in one place at /admin/journey.
type NewsJourney struct {
	NewsID          string    `gorm:"type:varchar(40);primaryKey" json:"news_id"`
	SourceURL       string    `gorm:"type:text;uniqueIndex;not null" json:"source_url"`
	SourceName      string    `gorm:"type:varchar(255)" json:"source_name"`
	Category        string    `gorm:"type:varchar(100)" json:"category"`
	OriginalTitle   string    `gorm:"type:text" json:"original_title"`
	OriginalContent string    `gorm:"type:text" json:"original_content"`
	ReceivedAt      time.Time `gorm:"index" json:"received_at"`
	PublishedStatus string    `gorm:"type:varchar(20);index" json:"published_status"` // pending | needs_editor | suspected_fake | rejected | published | pending_review | duplicate | updated
	ForceReal       bool      `gorm:"default:false" json:"force_real"`                // admin overrode a "fake" verdict; the next run must not reject it again

	FakeCheckStatus string     `gorm:"type:varchar(10)" json:"fake_check_status"` // real | fake
	FakeCheckReason string     `gorm:"type:text" json:"fake_check_reason"`
	FakeCheckAt     *time.Time `json:"fake_check_at"`

	CrossCheckSources string     `gorm:"type:text" json:"cross_check_sources"` // JSON array of {name,url}
	CrossCheckCount   int        `json:"cross_check_count"`
	CrossCheckAt      *time.Time `json:"cross_check_at"`

	RewrittenTitle   string     `gorm:"type:text" json:"rewritten_title"`
	RewrittenContent string     `gorm:"type:text" json:"rewritten_content"`
	RewrittenAt      *time.Time `json:"rewritten_at"`

	ArticleID   *uuid.UUID `gorm:"type:uuid" json:"article_id"`
	PublishedAt *time.Time `json:"published_at"`

	// Agentic desk fields.
	AgentQuestion string    `gorm:"type:text" json:"agent_question"` // the agent asked an editor before deciding
	EditorAnswer  string    `gorm:"type:text" json:"editor_answer"`  // the editor's reply, given to the agent on the next attempt
	AgentSummary  string    `gorm:"type:text" json:"agent_summary"`  // the agent's own note on what it decided and why
	AgentSteps    int       `json:"agent_steps"`
	AgentTokens   int       `json:"agent_tokens"`
	Attempts      int       `json:"attempts"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// AgentRun is one scheduled (or manual) pass of the agentic news desk.
type AgentRun struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	Trigger    string     `gorm:"type:varchar(30)" json:"trigger"` // n8n | admin | api
	Status     string     `gorm:"type:varchar(20);index" json:"status"`
	PlanNote   string     `gorm:"type:text" json:"plan_note"` // the editor agent's reasoning for what it chose to cover
	Candidates int        `json:"candidates"`
	Processed  int        `json:"processed"`
	Tokens     int        `json:"tokens"`
	Error      string     `gorm:"type:text" json:"error"`
	StartedAt  time.Time  `gorm:"index" json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// AgentStep is one step of the agent's work on a story — the model's turn
// (its note and the tools it chose) or a tool's result — so an editor can
// see exactly how the agent reached its decision.
type AgentStep struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	RunID     uuid.UUID `gorm:"type:uuid;index" json:"run_id"`
	NewsID    string    `gorm:"type:varchar(40);index" json:"news_id"`
	Step      int       `json:"step"`
	Kind      string    `gorm:"type:varchar(20)" json:"kind"` // plan | thought | tool | result | final | error
	Tool      string    `gorm:"type:varchar(60)" json:"tool"`
	Input     string    `gorm:"type:text" json:"input"`
	Output    string    `gorm:"type:text" json:"output"`
	Provider  string    `gorm:"type:varchar(30)" json:"provider"`
	Model     string    `gorm:"type:varchar(80)" json:"model"`
	Tokens    int       `json:"tokens"`
	CreatedAt time.Time `json:"created_at"`
}

// EditorFeedback records a human decision on the agent's work. The agent
// reads the most recent ones as long-term memory before each story.
type EditorFeedback struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	NewsID     string     `gorm:"type:varchar(40);index" json:"news_id"`
	ArticleID  *uuid.UUID `gorm:"type:uuid" json:"article_id"`
	Action     string     `gorm:"type:varchar(30)" json:"action"` // published | archived | rejected | requeued | answered
	Reason     string     `gorm:"type:text" json:"reason"`
	Title      string     `gorm:"type:text" json:"title"`
	SourceName string     `gorm:"type:varchar(255);index" json:"source_name"`
	CreatedAt  time.Time  `gorm:"index" json:"created_at"`
}

// ArticleVersion keeps the previous text of an article the agent updated
// when a developing story gained new facts.
type ArticleVersion struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ArticleID uuid.UUID `gorm:"type:uuid;index" json:"article_id"`
	Version   int       `json:"version"`
	Title     string    `gorm:"type:text" json:"title"`
	Content   string    `gorm:"type:text" json:"content"`
	Summary   string    `gorm:"type:text" json:"summary"`
	Note      string    `gorm:"type:text" json:"note"` // what changed and why
	CreatedAt time.Time `json:"created_at"`
}

// PopularStory is a story on a source outlet's own "most read" list, as last
// read from that outlet. The desk covers these first; the portal's "most
// read" box shows the ones it has published.
type PopularStory struct {
	URL       string    `gorm:"type:text;primaryKey" json:"url"`       // without query or fragment
	Outlet    string    `gorm:"type:varchar(255);index" json:"outlet"` // the feed's name, e.g. "Prothom Alo"
	Title     string    `gorm:"type:text" json:"title"`
	Rank      int       `json:"rank"` // 1 = most read
	Hits      int64     `json:"hits"` // the outlet's own read count, where it publishes one (0 = unknown)
	FetchedAt time.Time `gorm:"index" json:"fetched_at"`
}

// RankedArticle is a published article with its place on its source
// outlet's most-read list (Rank 0: not on one).
type RankedArticle struct {
	Article
	PopularOutlet string `json:"popular_outlet,omitempty"`
	PopularRank   int    `json:"popular_rank,omitempty"`
	PopularHits   int64  `json:"popular_hits,omitempty"`
}

// PopularKey is a URL as most-read lists are matched on: no query string
// (feeds add tracking parameters), fragment or trailing slash.
func PopularKey(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	return strings.TrimRight(strings.TrimSpace(u), "/")
}
