package cron

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"

	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

var (
	ErrJourneyDisabled = errors.New("journey tracking is not configured")
	ErrJourneyNotFound = errors.New("journey not found; send stage \"received\" first")
	ErrInvalidStage    = errors.New("invalid journey stage")
)

// SetJourneyRepo enables per-story journey tracking for the n8n pipeline.
func (w *Worker) SetJourneyRepo(repo repository.JourneyRepository) {
	w.journeyRepo = repo
}

// JourneySource is one other outlet the cross-check step found.
type JourneySource struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// JourneyUpdate is the body of PUT /api/n8n/journey. Stage picks which
// fields are used; the server stamps the matching *_at timestamp itself.
//
//	received    source_url, source_name, category, original_title, original_content
//	fake_check  is_fake, reason
//	suspected_fake  (AI flagged it; held until an admin decides)
//	rejected    reason (optional)
//	cross_check sources
//	rewritten   rewritten_title, rewritten_content
//
// The final "published" update is made by POST /api/n8n/articles when it is
// given the same news_id.
type JourneyUpdate struct {
	NewsID string `json:"news_id"`
	Stage  string `json:"stage"`

	SourceURL       string `json:"source_url"`
	SourceName      string `json:"source_name"`
	Category        string `json:"category"`
	OriginalTitle   string `json:"original_title"`
	OriginalContent string `json:"original_content"`

	IsFake *bool  `json:"is_fake"`
	Reason string `json:"reason"`

	Sources []JourneySource `json:"sources"`

	RewrittenTitle   string `json:"rewritten_title"`
	RewrittenContent string `json:"rewritten_content"`
}

// RecordJourney applies one stage update to the story's journey row.
func (w *Worker) RecordJourney(in JourneyUpdate) (*domain.NewsJourney, error) {
	if w.journeyRepo == nil {
		return nil, ErrJourneyDisabled
	}
	in.NewsID = strings.TrimSpace(in.NewsID)
	if in.NewsID == "" || len(in.NewsID) > 40 {
		return nil, fmt.Errorf("%w: news_id is required (max 40 chars)", ErrInvalidStage)
	}
	now := time.Now()

	var err error
	switch in.Stage {
	case "received":
		if u, perr := url.Parse(in.SourceURL); perr != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("%w: received needs an http(s) source_url", ErrInvalidStage)
		}
		err = w.journeyRepo.UpsertReceived(&domain.NewsJourney{
			NewsID:          in.NewsID,
			SourceURL:       in.SourceURL,
			SourceName:      in.SourceName,
			Category:        in.Category,
			OriginalTitle:   in.OriginalTitle,
			OriginalContent: in.OriginalContent,
			ReceivedAt:      now,
		})
	case "fake_check":
		if in.IsFake == nil {
			return nil, fmt.Errorf("%w: fake_check needs is_fake", ErrInvalidStage)
		}
		status := "real"
		if *in.IsFake {
			status = "fake"
		}
		err = w.journeyRepo.Update(in.NewsID, map[string]any{
			"fake_check_status": status, "fake_check_reason": in.Reason, "fake_check_at": &now,
		})
	case "suspected_fake":
		err = w.journeyRepo.Update(in.NewsID, map[string]any{"published_status": "suspected_fake"})
	case "rejected":
		err = w.journeyRepo.Update(in.NewsID, map[string]any{"published_status": "rejected"})
	case "cross_check":
		clean := make([]JourneySource, 0, len(in.Sources))
		seen := map[string]bool{}
		for _, s := range in.Sources {
			s.URL = strings.TrimSpace(s.URL)
			if s.URL == "" || seen[s.URL] {
				continue
			}
			seen[s.URL] = true
			clean = append(clean, JourneySource{Name: strings.TrimSpace(s.Name), URL: s.URL})
		}
		raw, _ := json.Marshal(clean)
		err = w.journeyRepo.Update(in.NewsID, map[string]any{
			"cross_check_sources": string(raw), "cross_check_count": len(clean), "cross_check_at": &now,
		})
	case "rewritten":
		err = w.journeyRepo.Update(in.NewsID, map[string]any{
			"rewritten_title": in.RewrittenTitle, "rewritten_content": in.RewrittenContent, "rewritten_at": &now,
		})
	default:
		return nil, fmt.Errorf("%w: %q (want received, fake_check, suspected_fake, rejected, cross_check, rewritten)", ErrInvalidStage, in.Stage)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrJourneyNotFound
	}
	if err != nil {
		return nil, err
	}
	return w.journeyRepo.Get(in.NewsID)
}

// RequeueJourney lets an admin overrule a "fake" verdict: the story goes back
// into the pending queue and the next run will not reject it again.
func (w *Worker) RequeueJourney(newsID string) error {
	if w.journeyRepo == nil {
		return ErrJourneyDisabled
	}
	return w.journeyRepo.Update(newsID, map[string]any{"published_status": "pending", "force_real": true})
}

// RejectJourney is an admin confirming a "suspected fake" verdict: the story
// is dropped for good (until re-queued).
func (w *Worker) RejectJourney(newsID string) error {
	if w.journeyRepo == nil {
		return ErrJourneyDisabled
	}
	return w.journeyRepo.Update(newsID, map[string]any{"published_status": "rejected"})
}

// ListJourneys pages through journey rows, newest first.
func (w *Worker) ListJourneys(status string, page, pageSize int) ([]domain.NewsJourney, int64, error) {
	if w.journeyRepo == nil {
		return nil, 0, ErrJourneyDisabled
	}
	if page < 1 {
		page = 1
	}
	return w.journeyRepo.List(status, pageSize, (page-1)*pageSize)
}

// finishJourney records the outcome of a save on the story's journey row.
// Best effort: a story saved without a journey row (e.g. an older workflow
// that never sent "received") is still saved.
func (w *Worker) finishJourney(newsID string, fields map[string]any) {
	if w.journeyRepo == nil || strings.TrimSpace(newsID) == "" {
		return
	}
	if err := w.journeyRepo.Update(newsID, fields); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("[n8n] failed to update journey %s: %v", newsID, err)
	}
}
