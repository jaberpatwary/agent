package repository

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"bangla-news-aggregator/internal/domain"
)

// JourneyRepository persists domain.NewsJourney rows — one per story, updated
// in place as the n8n pipeline moves it from stage to stage.
type JourneyRepository interface {
	// UpsertReceived creates the row, or resets an existing one back to
	// "pending" for a retry while keeping its original ReceivedAt.
	UpsertReceived(j *domain.NewsJourney) error
	// Update sets the given columns on the row; returns gorm.ErrRecordNotFound if it does not exist.
	Update(newsID string, fields map[string]any) error
	Get(newsID string) (*domain.NewsJourney, error)
	List(status string, limit, offset int) ([]domain.NewsJourney, int64, error)
	// Lookup returns the journey rows for the given source URLs, keyed by URL.
	Lookup(urls []string) (map[string]domain.NewsJourney, error)
}

type journeyRepository struct {
	db *gorm.DB
}

func NewJourneyRepository(db *gorm.DB) JourneyRepository {
	return &journeyRepository{db: db}
}

func (r *journeyRepository) UpsertReceived(j *domain.NewsJourney) error {
	j.PublishedStatus = "pending"
	if j.ReceivedAt.IsZero() {
		j.ReceivedAt = time.Now()
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "news_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source_name", "category", "original_title", "original_content", "published_status", "updated_at",
		}),
	}).Create(j).Error
}

func (r *journeyRepository) Update(newsID string, fields map[string]any) error {
	fields["updated_at"] = time.Now()
	res := r.db.Model(&domain.NewsJourney{}).Where("news_id = ?", newsID).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *journeyRepository) Get(newsID string) (*domain.NewsJourney, error) {
	var j domain.NewsJourney
	if err := r.db.First(&j, "news_id = ?", newsID).Error; err != nil {
		return nil, err
	}
	return &j, nil
}

func (r *journeyRepository) List(status string, limit, offset int) ([]domain.NewsJourney, int64, error) {
	q := r.db.Model(&domain.NewsJourney{})
	if status != "" {
		q = q.Where("published_status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []domain.NewsJourney
	err := q.Omit("original_content", "rewritten_content").
		Order("received_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

func (r *journeyRepository) Lookup(urls []string) (map[string]domain.NewsJourney, error) {
	out := make(map[string]domain.NewsJourney, len(urls))
	if len(urls) == 0 {
		return out, nil
	}
	var rows []domain.NewsJourney
	if err := r.db.Select("news_id", "source_url", "published_status", "force_real").
		Where("source_url IN ?", urls).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, j := range rows {
		out[j.SourceURL] = j
	}
	return out, nil
}
