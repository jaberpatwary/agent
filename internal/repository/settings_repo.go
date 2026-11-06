package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"bangla-news-aggregator/internal/domain"
)

// SettingsRepository persists small admin-editable configuration values,
// such as the AI rewrite house style, as key/value rows.
type SettingsRepository interface {
	Get(key string) (string, bool, error)
	Set(key, value string) error
}

type settingsRepository struct {
	db *gorm.DB
}

func NewSettingsRepository(db *gorm.DB) SettingsRepository {
	return &settingsRepository{db: db}
}

func (r *settingsRepository) Get(key string) (string, bool, error) {
	var row domain.AppSetting
	err := r.db.First(&row, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return row.Value, true, nil
}

func (r *settingsRepository) Set(key, value string) error {
	row := domain.AppSetting{Key: key, Value: value, UpdatedAt: time.Now()}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&row).Error
}
