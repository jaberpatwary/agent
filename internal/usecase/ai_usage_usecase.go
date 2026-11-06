package usecase

import (
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

// ProviderStat aggregates ai_usage rows per provider for the admin dashboard.
type ProviderStat struct {
	Provider      string
	TotalCalls    int
	SuccessCalls  int
	FailedCalls   int
	SuccessRate   float64 // 0..100
	TotalTokens   int
	AvgDurationMs int64
	LastError     string
}

// AIUsageUsecase summarizes recent AI provider activity so admins can see
// whether the agentic rewriter is actually working, which provider is being
// used, and roughly how much it costs — none of which was visible anywhere
// in the UI before, even though every call was already being logged.
type AIUsageUsecase interface {
	RecentStats(sampleSize int) (overall []ProviderStat, recent []domain.AIUsage, err error)
}

type aiUsageUsecase struct {
	repo repository.AIUsageRepository
}

func NewAIUsageUsecase(repo repository.AIUsageRepository) AIUsageUsecase {
	return &aiUsageUsecase{repo: repo}
}

func (u *aiUsageUsecase) RecentStats(sampleSize int) ([]ProviderStat, []domain.AIUsage, error) {
	if sampleSize <= 0 || sampleSize > 2000 {
		sampleSize = 500
	}
	rows, err := u.repo.Stats(sampleSize)
	if err != nil {
		return nil, nil, err
	}

	byProvider := map[string]*ProviderStat{}
	order := make([]string, 0, 4)
	for _, r := range rows {
		s, ok := byProvider[r.Provider]
		if !ok {
			s = &ProviderStat{Provider: r.Provider}
			byProvider[r.Provider] = s
			order = append(order, r.Provider)
		}
		s.TotalCalls++
		s.TotalTokens += r.TokensUsed
		s.AvgDurationMs += r.DurationMs
		if r.Success {
			s.SuccessCalls++
		} else {
			s.FailedCalls++
			if r.ErrorMsg != "" {
				s.LastError = r.ErrorMsg
			}
		}
	}

	overall := make([]ProviderStat, 0, len(order))
	for _, name := range order {
		s := byProvider[name]
		if s.TotalCalls > 0 {
			s.SuccessRate = float64(s.SuccessCalls) / float64(s.TotalCalls) * 100
			s.AvgDurationMs = s.AvgDurationMs / int64(s.TotalCalls)
		}
		overall = append(overall, *s)
	}

	recent := rows
	if len(recent) > 50 {
		recent = recent[:50]
	}

	return overall, recent, nil
}
