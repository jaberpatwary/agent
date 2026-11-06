package usecase

import (
	"errors"
	"strings"

	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/repository"
)

const (
	houseStyleKey          = "ai_house_style"
	sensitiveCategoriesKey = "ai_sensitive_categories"
)

// DefaultSensitiveCategories lists categories that are held for human
// approval instead of auto-publishing, until an admin changes this from
// /admin/style. "bangladesh" and "international" cover politics, law & order,
// and geopolitics — the areas where an unreviewed AI rewrite getting a
// detail wrong is most consequential.
const DefaultSensitiveCategories = "bangladesh,international"

// SettingsUsecase manages small admin-editable runtime settings — the AI
// rewrite house style and which categories require human approval before
// publishing — kept separate from Config so they can be changed from the
// admin UI without a redeploy.
type SettingsUsecase interface {
	GetHouseStyle() (string, error)
	SetHouseStyle(style string) error
	GetSensitiveCategories() ([]string, error)
	SetSensitiveCategories(categories []string) error
}

type settingsUsecase struct {
	repo repository.SettingsRepository
}

func NewSettingsUsecase(repo repository.SettingsRepository) SettingsUsecase {
	return &settingsUsecase{repo: repo}
}

func (u *settingsUsecase) GetHouseStyle() (string, error) {
	value, ok, err := u.repo.Get(houseStyleKey)
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(value) == "" {
		return ai.DefaultHouseStyle, nil
	}
	return value, nil
}

func (u *settingsUsecase) SetHouseStyle(style string) error {
	style = strings.TrimSpace(style)
	if len(style) < 10 {
		return errors.New("house style instructions look too short — add a bit more detail")
	}
	if len(style) > 4000 {
		return errors.New("house style instructions are too long (max 4000 characters)")
	}
	return u.repo.Set(houseStyleKey, style)
}

func (u *settingsUsecase) GetSensitiveCategories() ([]string, error) {
	value, ok, err := u.repo.Get(sensitiveCategoriesKey)
	if err != nil {
		return nil, err
	}
	if !ok || strings.TrimSpace(value) == "" {
		value = DefaultSensitiveCategories
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

func (u *settingsUsecase) SetSensitiveCategories(categories []string) error {
	cleaned := make([]string, 0, len(categories))
	for _, c := range categories {
		c = strings.ToLower(strings.TrimSpace(c))
		if c != "" {
			cleaned = append(cleaned, c)
		}
	}
	return u.repo.Set(sensitiveCategoriesKey, strings.Join(cleaned, ","))
}
