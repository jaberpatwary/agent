package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"bangla-news-aggregator/internal/domain"
)

// SeenURLRepository tracks every RSS item URL already folded into some
// article, including the secondary URLs of a multi-source synthesis (see
// domain.SeenSourceURL for why this can't just live on the articles table).
type SeenURLRepository interface {
	// FilterUnseen returns the subset of urls that have not been recorded yet.
	FilterUnseen(urls []string) ([]string, error)
	// RecordAll links every url to articleID. Safe to call with a url that is
	// already recorded (e.g. a race between two workers) — duplicates are
	// silently ignored rather than erroring the whole batch.
	RecordAll(articleID uuid.UUID, urls []string) error
}

type seenURLRepository struct {
	db *gorm.DB
}

func NewSeenURLRepository(db *gorm.DB) SeenURLRepository {
	return &seenURLRepository{db: db}
}

func (r *seenURLRepository) FilterUnseen(urls []string) ([]string, error) {
	if len(urls) == 0 {
		return nil, nil
	}
	var existing []string
	if err := r.db.Model(&domain.SeenSourceURL{}).
		Where("source_url IN ?", urls).
		Pluck("source_url", &existing).Error; err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(existing))
	for _, u := range existing {
		seen[u] = true
	}
	unseen := make([]string, 0, len(urls))
	for _, u := range urls {
		if !seen[u] {
			unseen = append(unseen, u)
		}
	}
	return unseen, nil
}

func (r *seenURLRepository) RecordAll(articleID uuid.UUID, urls []string) error {
	if len(urls) == 0 {
		return nil
	}
	rows := make([]domain.SeenSourceURL, 0, len(urls))
	for _, u := range urls {
		if u == "" {
			continue
		}
		rows = append(rows, domain.SeenSourceURL{SourceURL: u, ArticleID: articleID, CreatedAt: time.Now()})
	}
	if len(rows) == 0 {
		return nil
	}
	return r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}
