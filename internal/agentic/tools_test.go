package agentic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

type fakeNewsroom struct {
	reviewerPasses bool
	saved          []cron.ExternalArticle
	journey        []cron.JourneyUpdate
}

func (f *fakeNewsroom) RunSettings() (string, map[string]bool) { return "style", nil }
func (f *fakeNewsroom) SearchArticles(string, int) ([]domain.Article, error) {
	return nil, nil
}
func (f *fakeNewsroom) GetArticle(uuid.UUID) (*domain.Article, error) { return nil, nil }
func (f *fakeNewsroom) SaveExternal(_ context.Context, in cron.ExternalArticle) (*domain.Article, error) {
	f.saved = append(f.saved, in)
	status := "published"
	if in.ForceReview != "" {
		status = "pending_review"
	}
	return &domain.Article{ID: uuid.New(), Status: status, ReviewReason: in.ForceReview}, nil
}
func (f *fakeNewsroom) UpdateExternal(context.Context, uuid.UUID, cron.ExternalArticle, string, repository.AgentRepository) (*domain.Article, error) {
	return &domain.Article{ID: uuid.New(), Status: "published"}, nil
}
func (f *fakeNewsroom) RecordJourney(in cron.JourneyUpdate) (*domain.NewsJourney, error) {
	f.journey = append(f.journey, in)
	return &domain.NewsJourney{}, nil
}
func (f *fakeNewsroom) AIText(context.Context, ai.GatewayRequest) (ai.Result, error) {
	if f.reviewerPasses {
		return ai.Result{Text: `{"passed":true,"scores":{"accuracy":5},"issues":[]}`}, nil
	}
	return ai.Result{Text: `{"passed":false,"issues":["৪৫ জন আহতের তথ্য নেই"]}`}, nil
}

func newTestStory(nr *fakeNewsroom, autopublish bool) (*story, map[string]Tool, *map[string]any) {
	journeyFields := map[string]any{}
	s := &story{
		item: cron.PendingItem{NewsID: "n1", Title: "মিরপুরে আগুন", Content: origStory, SourceName: "Feed", Category: "general", ImageURL: "/img/x.jpg"},
		nr:   nr,
		searchNews: func(context.Context, string, string) ([]NewsHit, error) {
			return []NewsHit{{Title: "মিরপুরে কারখানায় আগুন", Source: "Other Outlet", URL: "https://example.com/a"}}, nil
		},
		readPage: func(context.Context, string) (string, error) { return "", nil },
		setJourney: func(f map[string]any) {
			for k, v := range f {
				journeyFields[k] = v
			}
		},
		autopublish: autopublish,
	}
	byName := map[string]Tool{}
	for _, t := range s.tools() {
		byName[t.Spec.Name] = t
	}
	return s, byName, &journeyFields
}

func run(t *testing.T, tools map[string]Tool, name, args string) (string, error) {
	t.Helper()
	return tools[name].Run(context.Background(), json.RawMessage(args))
}

const goodDraft = `{"title":"মিরপুরের পোশাক কারখানায় আগুন, আহত ৪৫","summary":"সারাংশ","tags":["আগুন"],
"body":"<p>রাজধানীর মিরপুর এলাকায় বৃহস্পতিবার সকালের দিকে এক পোশাক তৈরির কারখানায় বড় ধরনের অগ্নিকাণ্ড ঘটেছে, ফায়ার সার্ভিস এ তথ্য নিশ্চিত করেছে।</p><p>আগুন নেভাতে ফায়ার সার্ভিসের ১২টি দল প্রায় দুই ঘণ্টা কাজ করে, এ ঘটনায় কমপক্ষে ৪৫ জন শ্রমিক আহত হয়েছেন বলে জানা গেছে।</p><p>কারখানা কর্তৃপক্ষের হিসাবে ক্ষতির পরিমাণ আনুমানিক ৩০০ কোটি টাকা, আর ঘটনার কারণ খুঁজতে একটি তদন্ত কমিটি করা হয়েছে।</p>"}`

func TestGuardrailsForceEvidenceBeforeVerdicts(t *testing.T) {
	_, tools, _ := newTestStory(&fakeNewsroom{}, true)
	if _, err := run(t, tools, "record_verification", `{"is_real":true,"reason":"x"}`); err == nil {
		t.Fatal("a verdict without any search must be refused")
	}
	if _, err := run(t, tools, "reject_story", `{"kind":"fake","explanation":"looks wrong"}`); err == nil {
		t.Fatal("calling a story fake without searching and evidence must be refused")
	}
}

func TestGuardrailsOnPublishing(t *testing.T) {
	nr := &fakeNewsroom{reviewerPasses: false}
	_, tools, _ := newTestStory(nr, true)
	if _, err := run(t, tools, "submit_article", `{"decision":"publish","note":"x"}`); err == nil || !strings.Contains(err.Error(), "review_draft") {
		t.Fatalf("submitting with no draft must be refused, got %v", err)
	}
	mustRun(t, tools, "search_news", `{"query":"মিরপুর আগুন"}`)
	mustRun(t, tools, "record_verification", `{"is_real":true,"reason":"অন্য সংবাদমাধ্যমেও এসেছে"}`)
	out := mustRun(t, tools, "review_draft", goodDraft)
	if !strings.Contains(out, `"passed": false`) {
		t.Fatalf("the AI editor rejected it, so the review must fail: %s", out)
	}
	if _, err := run(t, tools, "submit_article", `{"decision":"publish","note":"x"}`); err == nil {
		t.Fatal("publishing a draft whose review failed must be refused")
	}
	if _, err := run(t, tools, "submit_article", `{"decision":"hold","note":"রিভিউ পাস হয়নি"}`); err != nil {
		t.Fatalf("holding a failed draft for an editor should be allowed: %v", err)
	}
	if len(nr.saved) != 1 || nr.saved[0].ForceReview == "" {
		t.Fatalf("a held draft must be saved for review, got %+v", nr.saved)
	}
}

func TestHappyPathPublishesAndRespectsAutopublishSwitch(t *testing.T) {
	for _, autopublish := range []bool{true, false} {
		nr := &fakeNewsroom{reviewerPasses: true}
		_, tools, _ := newTestStory(nr, autopublish)
		mustRun(t, tools, "search_news", `{"query":"মিরপুর আগুন"}`)
		mustRun(t, tools, "record_verification", `{"is_real":true,"reason":"নিশ্চিত"}`)
		if out := mustRun(t, tools, "review_draft", goodDraft); !strings.Contains(out, `"passed": true`) {
			t.Fatalf("good draft should pass: %s", out)
		}
		out := mustRun(t, tools, "submit_article", `{"decision":"publish","note":"যাচাই করা হয়েছে"}`)
		wantHeld := !autopublish
		if held := nr.saved[0].ForceReview != ""; held != wantHeld {
			t.Fatalf("autopublish=%v: held=%v (%s)", autopublish, held, out)
		}
		// Sources found while searching are recorded for the journey page.
		var cross *cron.JourneyUpdate
		for i := range nr.journey {
			if nr.journey[i].Stage == "cross_check" {
				cross = &nr.journey[i]
			}
		}
		if cross == nil || len(cross.Sources) != 1 || cross.Sources[0].Name != "Other Outlet" {
			t.Fatalf("corroborating outlets should be recorded, got %+v", cross)
		}
	}
}

func TestAskEditorPausesTheStory(t *testing.T) {
	_, tools, fields := newTestStory(&fakeNewsroom{}, true)
	mustRun(t, tools, "ask_editor", `{"question":"অভিযোগটি কি প্রকাশ করা নিরাপদ?"}`)
	if (*fields)["published_status"] != "needs_editor" || (*fields)["agent_question"] == "" {
		t.Fatalf("story should wait for the editor, got %v", *fields)
	}
}

func mustRun(t *testing.T, tools map[string]Tool, name, args string) string {
	t.Helper()
	out, err := run(t, tools, name, args)
	if err != nil {
		t.Fatalf("%s failed: %v", name, err)
	}
	return out
}
