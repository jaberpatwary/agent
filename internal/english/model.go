// Package english is the English edition of the portal: The Daily Star's
// English site (www.thedailystar.net) as a layout, filled with stories from
// Bangladesh's English-language outlets, plus a daily e-paper made from them.
//
// It is separate from the Bangla portal on purpose: its own tables, its own
// templates (web/en) and its own routes (/en, /epaper). The Bangla portal's
// code, templates and data are not touched.
package english

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Article is one English story.
type Article struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	Title       string    `gorm:"size:500"`
	Summary     string    `gorm:"type:text"`
	Content     string    `gorm:"type:text"` // the story in our words, as <p> paragraphs; empty for a brief
	ImageURL    string    `gorm:"size:1000"`
	SourceName  string    `gorm:"size:100"`
	SourceURL   string    `gorm:"size:1000;uniqueIndex"`
	Section     string    `gorm:"size:40;index"`
	Sub         string    `gorm:"size:60;index"`
	Rewritten   bool
	RewrittenAt *time.Time // when the AI wrote it, for the day's cap
	PublishedAt time.Time  `gorm:"index"`
	CreatedAt   time.Time
}

func (Article) TableName() string { return "en_articles" }

// BeforeCreate gives a story an id.
func (a *Article) BeforeCreate(*gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

// Brief reports whether only a summary may be shown (the story could not be
// rewritten), so the page links to the full story at its source.
func (a Article) Brief() bool { return a.Content == "" }

// Seen records a source address already looked at, so a story that could not
// be used is not fetched again on every run.
type Seen struct {
	URL       string `gorm:"primaryKey;size:1000"`
	CreatedAt time.Time
}

func (Seen) TableName() string { return "en_seen" }

// Edition is one day's e-paper: the stories on each page, in order.
type Edition struct {
	Date      string `gorm:"primaryKey;size:10"` // 2006-01-02, Dhaka time
	Pages     string `gorm:"type:text"`          // JSON []EditionPage
	Layout    int    // the layout rules it was made with (editionLayout)
	CreatedAt time.Time
}

func (Edition) TableName() string { return "en_editions" }

// EpaperRead counts how often a story of a day's e-paper was opened, for the
// e-paper's "Most Popular Article" list.
type EpaperRead struct {
	Date      string `gorm:"primaryKey;size:10"`
	ArticleID string `gorm:"primaryKey;size:36"`
	Count     int
}

func (EpaperRead) TableName() string { return "en_epaper_reads" }

// EditionPage is one printed page of an edition.
type EditionPage struct {
	No      int      `json:"no"`
	Name    string   `json:"name"`
	Group   string   `json:"group,omitempty"` // a supplement (Business, Lifestyle); "" is the Dhaka Edition
	Stories []string `json:"stories"`         // article ids, the lead first
}
