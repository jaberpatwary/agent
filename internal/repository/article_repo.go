package repository

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"bangla-news-aggregator/internal/domain"
)

var ErrDuplicateArticle = errors.New("duplicate article")

// ArticleRepository defines persistence operations for articles.
type ArticleRepository interface {
	Create(a *domain.Article) error
	ExistsBySourceURL(sourceURL string) (bool, error)
	FilterUnseenSourceURLs(urls []string) ([]string, error)
	ExistsByTitleAndDate(title string, publishedAt time.Time) (bool, error)
	GetByID(id uuid.UUID) (*domain.Article, error)
	List(limit, offset int, category, status string) ([]domain.Article, int64, error)
	Search(limit, offset int, query, category, status string) ([]domain.Article, int64, error)
	Delete(id uuid.UUID) error
	UpdateStatus(id uuid.UUID, status string) error
	RelatedArticles(excludeID uuid.UUID, tags []string, category string, limit int) ([]domain.Article, error)
	IncrementViews(id uuid.UUID) error
	MostRead(since time.Time, limit int) ([]domain.Article, error)
	RecentTags(limit int) ([]string, error)
	UpdateStory(id uuid.UUID, title, content, summary, tags string) error
}

type articleRepository struct {
	db *gorm.DB
}

func NewArticleRepository(db *gorm.DB) ArticleRepository {
	return &articleRepository{db: db}
}

func (r *articleRepository) Create(a *domain.Article) error {
	result := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(a)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrDuplicateArticle
	}
	return nil
}

func (r *articleRepository) ExistsBySourceURL(sourceURL string) (bool, error) {
	var count int64
	err := r.db.Model(&domain.Article{}).Where("source_url = ?", sourceURL).Count(&count).Error
	return count > 0, err
}

// FilterUnseenSourceURLs returns the subset of urls that are not yet any
// article's primary SourceURL. Used as the (legacy-compatible) first pass of
// the cron pipeline's dedupe check, alongside SeenURLRepository which also
// covers the secondary URLs folded into a multi-source synthesis.
func (r *articleRepository) FilterUnseenSourceURLs(urls []string) ([]string, error) {
	if len(urls) == 0 {
		return nil, nil
	}
	var existing []string
	if err := r.db.Model(&domain.Article{}).Where("source_url IN ?", urls).Pluck("source_url", &existing).Error; err != nil {
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

func (r *articleRepository) ExistsByTitleAndDate(title string, publishedAt time.Time) (bool, error) {
	var count int64
	err := r.db.Model(&domain.Article{}).
		Where(`lower(regexp_replace(trim(title), '[[:space:][:punct:]]+', ' ', 'g')) = lower(regexp_replace(trim(?), '[[:space:][:punct:]]+', ' ', 'g'))`, title).
		Where(`(published_at AT TIME ZONE 'Asia/Dhaka')::date = (?::timestamptz AT TIME ZONE 'Asia/Dhaka')::date`, publishedAt).
		Count(&count).Error
	return count > 0, err
}

func (r *articleRepository) GetByID(id uuid.UUID) (*domain.Article, error) {
	var a domain.Article
	err := r.db.First(&a, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &a, err
}

func (r *articleRepository) List(limit, offset int, category, status string) ([]domain.Article, int64, error) {
	var articles []domain.Article
	var total int64

	q := r.db.Model(&domain.Article{})
	if category != "" {
		q = q.Where("category = ?", category)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}

	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := q.Select(`articles.*, ROW_NUMBER() OVER (PARTITION BY source_name ORDER BY published_at DESC, created_at DESC) AS source_rank`).
		Order(`source_rank ASC, published_at DESC, created_at DESC`).
		Limit(limit).Offset(offset).Find(&articles).Error
	return articles, total, err
}

func (r *articleRepository) Search(limit, offset int, query, category, status string) ([]domain.Article, int64, error) {
	var articles []domain.Article
	var total int64
	pattern := "%" + query + "%"
	q := r.db.Model(&domain.Article{}).Where(
		"(title ILIKE ? OR summary ILIKE ? OR content ILIKE ?)", pattern, pattern, pattern,
	)
	if category != "" {
		q = q.Where("category = ?", category)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Select(`articles.*, ROW_NUMBER() OVER (PARTITION BY source_name ORDER BY published_at DESC, created_at DESC) AS source_rank`).
		Order(`source_rank ASC, published_at DESC, created_at DESC`).
		Limit(limit).Offset(offset).Find(&articles).Error
	return articles, total, err
}

func (r *articleRepository) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.Article{}, "id = ?", id).Error
}

func (r *articleRepository) UpdateStatus(id uuid.UUID, status string) error {
	if status == "published" {
		// Going live now (e.g. an editor approved a held story): the portal
		// lists news newest first, so it must show as new news, not at the
		// time its source first ran it.
		return r.db.Model(&domain.Article{}).Where("id = ? AND status <> ?", id, "published").
			Updates(map[string]any{"status": status, "published_at": time.Now()}).Error
	}
	return r.db.Model(&domain.Article{}).Where("id = ?", id).Update("status", status).Error
}

// RelatedArticles finds other published articles sharing at least one tag
// with the source article, newest first; if tags is empty (or finds nothing)
// it falls back to same-category articles instead of returning nothing.
func (r *articleRepository) RelatedArticles(excludeID uuid.UUID, tags []string, category string, limit int) ([]domain.Article, error) {
	var articles []domain.Article

	if len(tags) > 0 {
		q := r.db.Model(&domain.Article{}).
			Where("status = ?", "published").
			Where("id <> ?", excludeID)
		conds := r.db
		for i, t := range tags {
			cond := "tags ILIKE ?"
			val := "%" + t + "%"
			if i == 0 {
				conds = conds.Where(cond, val)
			} else {
				conds = conds.Or(cond, val)
			}
		}
		err := q.Where(conds).Order("published_at DESC").Limit(limit).Find(&articles).Error
		if err != nil {
			return nil, err
		}
	}

	if len(articles) < limit && category != "" {
		have := make(map[string]bool, len(articles))
		for _, a := range articles {
			have[a.ID.String()] = true
		}
		var more []domain.Article
		err := r.db.Model(&domain.Article{}).
			Where("status = ?", "published").
			Where("id <> ?", excludeID).
			Where("category = ?", category).
			Order("published_at DESC").
			Limit(limit).
			Find(&more).Error
		if err != nil {
			return articles, err
		}
		for _, a := range more {
			if len(articles) >= limit {
				break
			}
			if !have[a.ID.String()] {
				articles = append(articles, a)
				have[a.ID.String()] = true
			}
		}
	}

	return articles, nil
}

// IncrementViews bumps a published article's view counter by one.
func (r *articleRepository) IncrementViews(id uuid.UUID) error {
	return r.db.Model(&domain.Article{}).Where("id = ?", id).
		UpdateColumn("views", gorm.Expr("views + 1")).Error
}

// MostRead returns the most viewed published articles published since the given time.
func (r *articleRepository) MostRead(since time.Time, limit int) ([]domain.Article, error) {
	var out []domain.Article
	err := r.db.Where("status = ? AND published_at >= ? AND views > 0", "published", since).
		Order("views DESC, published_at DESC").Limit(limit).Find(&out).Error
	return out, err
}

// RecentTags returns the raw tag strings of the newest published articles.
func (r *articleRepository) RecentTags(limit int) ([]string, error) {
	var tags []string
	err := r.db.Model(&domain.Article{}).Where("status = ? AND tags <> ''", "published").
		Order("published_at DESC").Limit(limit).Pluck("tags", &tags).Error
	return tags, err
}

// UpdateStory replaces an article's text (used when the agent updates a
// developing story with new facts; the old text is kept as an ArticleVersion).
func (r *articleRepository) UpdateStory(id uuid.UUID, title, content, summary, tags string) error {
	return r.db.Model(&domain.Article{}).Where("id = ?", id).Updates(map[string]any{
		"title": title, "content": content, "summary": summary, "tags": tags, "updated_at": time.Now(),
	}).Error
}
