package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"bangla-news-aggregator/internal/domain"
)

// FeedRepository defines persistence operations for RSS feed sources.
type FeedRepository interface {
	Create(f *domain.FeedSource) error
	ListActive() ([]domain.FeedSource, error)
	List() ([]domain.FeedSource, error)
	Delete(id uuid.UUID) error
	SetActive(id uuid.UUID, active bool) error
	RecordFetch(id uuid.UUID, fetchErr error) error
}

type feedRepository struct {
	db *gorm.DB
}

func NewFeedRepository(db *gorm.DB) FeedRepository {
	return &feedRepository{db: db}
}

func (r *feedRepository) Create(f *domain.FeedSource) error {
	return r.db.Create(f).Error
}

func (r *feedRepository) ListActive() ([]domain.FeedSource, error) {
	var feeds []domain.FeedSource
	err := r.db.Where("active = ?", true).Find(&feeds).Error
	return feeds, err
}

func (r *feedRepository) List() ([]domain.FeedSource, error) {
	var feeds []domain.FeedSource
	err := r.db.Order("created_at DESC").Find(&feeds).Error
	return feeds, err
}

func (r *feedRepository) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.FeedSource{}, "id = ?", id).Error
}

func (r *feedRepository) SetActive(id uuid.UUID, active bool) error {
	return r.db.Model(&domain.FeedSource{}).Where("id = ?", id).Update("active", active).Error
}

func (r *feedRepository) RecordFetch(id uuid.UUID, fetchErr error) error {
	updates := map[string]interface{}{
		"last_fetch": time.Now(),
	}
	if fetchErr != nil {
		updates["last_error"] = fetchErr.Error()
	} else {
		updates["last_error"] = ""
	}
	return r.db.Model(&domain.FeedSource{}).Where("id = ?", id).Updates(updates).Error
}
