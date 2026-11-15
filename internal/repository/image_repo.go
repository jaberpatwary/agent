package repository

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"bangla-news-aggregator/internal/domain"
)

// ImageRepository keeps copies of cached photos in the database; see
// domain.StoredImage. It satisfies imagestore.Backup.
type ImageRepository struct{ db *gorm.DB }

func NewImageRepository(db *gorm.DB) *ImageRepository { return &ImageRepository{db: db} }

// Save stores (or replaces) a photo under its file name.
func (r *ImageRepository) Save(name, contentType string, data []byte) error {
	img := domain.StoredImage{Name: name, ContentType: contentType, Data: data}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"content_type", "data"}),
	}).Create(&img).Error
}

// Load returns a stored photo's bytes and content type.
func (r *ImageRepository) Load(name string) ([]byte, string, error) {
	var img domain.StoredImage
	if err := r.db.Where("name = ?", name).First(&img).Error; err != nil {
		return nil, "", err
	}
	return img.Data, img.ContentType, nil
}
