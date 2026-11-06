package usecase

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

type FeedUsecase interface {
	Create(name, url, category string) (*domain.FeedSource, error)
	List() ([]domain.FeedSource, error)
	Delete(id uuid.UUID) error
	SetActive(id uuid.UUID, active bool) error
}

type feedUsecase struct {
	repo repository.FeedRepository
}

func NewFeedUsecase(repo repository.FeedRepository) FeedUsecase {
	return &feedUsecase{repo: repo}
}

func (u *feedUsecase) Create(name, rawURL, category string) (*domain.FeedSource, error) {
	name = strings.TrimSpace(name)
	category = strings.TrimSpace(category)
	canonicalURL, err := validateFeedURL(rawURL)
	if name == "" || err != nil {
		return nil, errors.New("feed must have a name and a valid public http/https URL")
	}
	f := &domain.FeedSource{
		Name:     name,
		URL:      canonicalURL,
		Category: category,
		Active:   true,
	}
	if err := u.repo.Create(f); err != nil {
		return nil, err
	}
	return f, nil
}

func (u *feedUsecase) List() ([]domain.FeedSource, error) {
	return u.repo.List()
}

func (u *feedUsecase) Delete(id uuid.UUID) error {
	return u.repo.Delete(id)
}

func (u *feedUsecase) SetActive(id uuid.UUID, active bool) error {
	return u.repo.SetActive(id, active)
}

// validateFeedURL blocks obvious local/private targets. Production deployments
// should also apply outbound network restrictions to prevent DNS rebinding.
func validateFeedURL(rawURL string) (string, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "", errors.New("invalid feed URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal" {
		return "", errors.New("local and metadata hostnames are not allowed")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()) {
		return "", errors.New("private or local IP addresses are not allowed as feed URLs")
	}
	return parsed.String(), nil
}
