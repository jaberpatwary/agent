package repository

import (
	"time"

	"gorm.io/gorm"

	"bangla-news-aggregator/internal/domain"
)

// PopularRepository keeps the source outlets' current most-read lists.
type PopularRepository interface {
	// Replace swaps one outlet's list for a freshly read one.
	Replace(outlet string, list []domain.PopularStory) error
	// Published returns published articles whose source is on a most-read
	// list read since the given time, best ranked first.
	Published(since time.Time, limit int) ([]domain.RankedArticle, error)
	// RankOf returns the outlet and rank of a source URL, if it is on a list.
	RankOf(sourceURL string, since time.Time) (string, int, error)
	Handled(keys []string) (map[string]bool, error)
}

type popularRepository struct{ db *gorm.DB }

func NewPopularRepository(db *gorm.DB) PopularRepository { return &popularRepository{db: db} }

// sourceKey is an article's source URL as the lists store it. chr(63) is
// "?": GORM would take a quoted "?" for a parameter.
const sourceKey = `rtrim(split_part(split_part(a.source_url, '#', 1), chr(63), 1), '/')`

func (r *popularRepository) Replace(outlet string, list []domain.PopularStory) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("outlet = ?", outlet).Delete(&domain.PopularStory{}).Error; err != nil {
			return err
		}
		if len(list) == 0 {
			return nil
		}
		// The same story can sit on two outlets' lists; the newest read wins.
		return tx.Save(&list).Error
	})
}

func (r *popularRepository) Published(since time.Time, limit int) ([]domain.RankedArticle, error) {
	var out []domain.RankedArticle
	err := r.db.Table("articles AS a").
		Select("a.*, p.outlet AS popular_outlet, p.rank AS popular_rank").
		Joins("JOIN popular_stories p ON p.url = "+sourceKey).
		Where("a.status = ? AND p.fetched_at >= ?", "published", since).
		Order("p.rank ASC, a.published_at DESC").
		Limit(limit).Scan(&out).Error
	return out, err
}

func (r *popularRepository) RankOf(sourceURL string, since time.Time) (string, int, error) {
	var p domain.PopularStory
	err := r.db.Where("url = ? AND fetched_at >= ?", domain.PopularKey(sourceURL), since).Limit(1).Find(&p).Error
	return p.Outlet, p.Rank, err
}

// Handled returns which of the given URLs (as lists store them) the desk has
// already dealt with: saved as an article, or rejected, held or sent to an
// editor. Feeds add tracking parameters, so the same story can come under
// another URL.
func (r *popularRepository) Handled(keys []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(keys) == 0 {
		return out, nil
	}
	var found []string
	err := r.db.Raw(`SELECT k FROM (
		SELECT `+sourceKey+` AS k FROM articles a
		UNION
		SELECT rtrim(split_part(split_part(j.source_url, '#', 1), chr(63), 1), '/') FROM news_journeys j
		WHERE j.published_status NOT IN ('', 'pending')
	) t WHERE k IN ?`, keys).Scan(&found).Error
	for _, k := range found {
		out[k] = true
	}
	return out, err
}
