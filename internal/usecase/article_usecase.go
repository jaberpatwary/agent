package usecase

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

type ArticleUsecase interface {
	List(page, pageSize int, category, status string) ([]domain.Article, int64, error)
	Search(page, pageSize int, query, category, status string) ([]domain.Article, int64, error)
	GetByID(id uuid.UUID) (*domain.Article, error)
	Delete(id uuid.UUID) error
	UpdateStatus(id uuid.UUID, status string) error
	Related(a *domain.Article, limit int) ([]domain.Article, error)
	RecordView(id uuid.UUID)
	MostRead(category string, limit int) ([]domain.Article, error)
	TrendingTags(limit int) ([]string, error)
}

type articleUsecase struct {
	repo repository.ArticleRepository
}

func NewArticleUsecase(repo repository.ArticleRepository) ArticleUsecase {
	return &articleUsecase{repo: repo}
}

func (u *articleUsecase) List(page, pageSize int, category, status string) ([]domain.Article, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	return u.repo.List(pageSize, offset, category, status)
}

func (u *articleUsecase) Search(page, pageSize int, query, category, status string) ([]domain.Article, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	query = strings.TrimSpace(query)
	if len([]rune(query)) > 100 {
		query = string([]rune(query)[:100])
	}
	if query == "" {
		return u.List(page, pageSize, category, status)
	}
	return u.repo.Search(pageSize, (page-1)*pageSize, query, category, status)
}

func (u *articleUsecase) GetByID(id uuid.UUID) (*domain.Article, error) {
	return u.repo.GetByID(id)
}

func (u *articleUsecase) Delete(id uuid.UUID) error {
	return u.repo.Delete(id)
}

var validArticleStatuses = map[string]bool{
	"draft": true, "published": true, "pending_review": true, "archived": true,
}

func (u *articleUsecase) UpdateStatus(id uuid.UUID, status string) error {
	if !validArticleStatuses[status] {
		return errors.New("status must be draft, published, pending_review, or archived")
	}
	return u.repo.UpdateStatus(id, status)
}

// Related returns other published articles that share at least one tag (or,
// failing that, the same category) with the given article — the AI-assigned
// tags (see internal/agent) are the main signal; category is a fallback for
// articles that predate tagging or where the model returned none.
func (u *articleUsecase) Related(a *domain.Article, limit int) ([]domain.Article, error) {
	if limit <= 0 || limit > 12 {
		limit = 4
	}
	var tags []string
	for _, t := range strings.Split(a.Tags, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return u.repo.RelatedArticles(a.ID, tags, a.Category, limit)
}

// RecordView counts one page view. Best effort: a failed counter update
// must never break the article page.
func (u *articleUsecase) RecordView(id uuid.UUID) {
	_ = u.repo.IncrementViews(id)
}

// MostRead is the "most read" list: the last 48 hours, widened to a week
// when the site is quiet and the short window has too few stories.
func (u *articleUsecase) MostRead(category string, limit int) ([]domain.Article, error) {
	list, err := u.repo.MostRead(category, time.Now().Add(-48*time.Hour), limit)
	if err != nil || len(list) >= limit {
		return list, err
	}
	return u.repo.MostRead(category, time.Now().Add(-7*24*time.Hour), limit)
}

// TrendingTags counts topic tags across the newest published stories and
// returns the most frequent ones (only tags that appear more than once).
func (u *articleUsecase) TrendingTags(limit int) ([]string, error) {
	raw, err := u.repo.RecentTags(80)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	display := map[string]string{}
	for _, tagList := range raw {
		for _, t := range strings.Split(tagList, ",") {
			t = strings.TrimSpace(t)
			if t == "" || len([]rune(t)) > 30 {
				continue
			}
			key := strings.ToLower(t)
			counts[key]++
			if _, ok := display[key]; !ok {
				display[key] = t
			}
		}
	}
	keys := make([]string, 0, len(counts))
	for k, n := range counts {
		if n > 1 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > limit {
		keys = keys[:limit]
	}
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = display[k]
	}
	return out, nil
}
