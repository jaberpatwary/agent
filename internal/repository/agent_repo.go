package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"bangla-news-aggregator/internal/domain"
)

// SourceRecord is the desk's track record with one outlet — part of the
// agent's long-term memory ("how often was this source wrong before?").
type SourceRecord struct {
	Source        string
	Stories       int
	Published     int
	Rejected      int // rejected as fake/duplicate/not news
	EditorRejects int // an editor overruled the agent and pulled the story
}

// AgentRepository persists the agentic desk's runs, step traces, editor
// feedback (memory) and article versions.
type AgentRepository interface {
	CreateRun(r *domain.AgentRun) error
	SaveRun(r *domain.AgentRun) error
	RecentRuns(limit int) ([]domain.AgentRun, error)
	AddStep(s *domain.AgentStep) error
	StepsForNews(newsID string) ([]domain.AgentStep, error)
	AddFeedback(f *domain.EditorFeedback) error
	RecentFeedback(limit int) ([]domain.EditorFeedback, error)
	SourceRecord(source string) (SourceRecord, error)
	SaveVersion(v *domain.ArticleVersion) error
	NextVersion(articleID uuid.UUID) (int, error)
}

type agentRepository struct{ db *gorm.DB }

func NewAgentRepository(db *gorm.DB) AgentRepository { return &agentRepository{db: db} }

func (r *agentRepository) CreateRun(run *domain.AgentRun) error {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	return r.db.Create(run).Error
}

func (r *agentRepository) SaveRun(run *domain.AgentRun) error { return r.db.Save(run).Error }

func (r *agentRepository) RecentRuns(limit int) ([]domain.AgentRun, error) {
	var out []domain.AgentRun
	err := r.db.Order("started_at DESC").Limit(limit).Find(&out).Error
	return out, err
}

func (r *agentRepository) AddStep(s *domain.AgentStep) error {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	return r.db.Create(s).Error
}

func (r *agentRepository) StepsForNews(newsID string) ([]domain.AgentStep, error) {
	var out []domain.AgentStep
	err := r.db.Where("news_id = ?", newsID).Order("created_at ASC, id ASC").Find(&out).Error
	return out, err
}

func (r *agentRepository) AddFeedback(f *domain.EditorFeedback) error {
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now()
	}
	return r.db.Create(f).Error
}

func (r *agentRepository) RecentFeedback(limit int) ([]domain.EditorFeedback, error) {
	var out []domain.EditorFeedback
	err := r.db.Order("created_at DESC").Limit(limit).Find(&out).Error
	return out, err
}

func (r *agentRepository) SourceRecord(source string) (SourceRecord, error) {
	rec := SourceRecord{Source: source}
	var rows []struct {
		Status string
		N      int
	}
	if err := r.db.Model(&domain.NewsJourney{}).Select("published_status AS status, count(*) AS n").
		Where("source_name = ?", source).Group("published_status").Scan(&rows).Error; err != nil {
		return rec, err
	}
	for _, row := range rows {
		rec.Stories += row.N
		switch row.Status {
		case "published", "pending_review", "updated":
			rec.Published += row.N
		case "rejected", "suspected_fake", "duplicate":
			rec.Rejected += row.N
		}
	}
	var pulled int64
	if err := r.db.Model(&domain.EditorFeedback{}).Where("source_name = ? AND action IN ?", source, []string{"archived", "rejected"}).
		Count(&pulled).Error; err != nil {
		return rec, err
	}
	rec.EditorRejects = int(pulled)
	return rec, nil
}

func (r *agentRepository) SaveVersion(v *domain.ArticleVersion) error {
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now()
	}
	return r.db.Create(v).Error
}

func (r *agentRepository) NextVersion(articleID uuid.UUID) (int, error) {
	var n int64
	err := r.db.Model(&domain.ArticleVersion{}).Where("article_id = ?", articleID).Count(&n).Error
	return int(n) + 1, err
}
