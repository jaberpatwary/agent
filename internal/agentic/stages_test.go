package agentic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

type fakeAgents struct {
	repository.AgentRepository
	steps []domain.AgentStep
}

func (f *fakeAgents) SaveRun(*domain.AgentRun) error { return nil }
func (f *fakeAgents) AddStep(s *domain.AgentStep) error {
	f.steps = append(f.steps, *s)
	return nil
}

// stagedStory is a verified story waiting for the writer, as the verify stage leaves it.
func stagedStory(t *testing.T, nr *fakeNewsroom, chat ChatFunc) (*Desk, *session, *story) {
	t.Helper()
	s, tools, _ := newTestStory(nr, true)
	mustRun(t, tools, "search_news", `{"query":"মিরপুর আগুন"}`)
	mustRun(t, tools, "record_verification", `{"is_real":true,"reason":"x","writer_notes":"আহতদের হাসপাতালের নাম যোগ করো"}`)
	d := &Desk{agents: &fakeAgents{}, chat: chat}
	sess := &session{run: &domain.AgentRun{StartedAt: time.Now()}, cfg: DeskSettings{StoriesPerRun: 1, StoryBudget: 100000}}
	j := &job{s: s, system: "sys"}
	j.record = func(StepRecord) { j.steps++ }
	sess.job = j
	return d, sess, s
}

func TestVerifyStageEndsOnlyOnAPositiveVerdict(t *testing.T) {
	s, _, _ := newTestStory(&fakeNewsroom{}, true)
	both := call("record_verification", `{"is_real":true,"reason":"অন্য সংবাদমাধ্যমেও আছে"}`)
	both.ToolCalls = append(both.ToolCalls, ai.ToolCall{ID: "late", Name: "search_news", Args: []byte(`{"query":"x"}`)})
	chat, _ := scriptedModel(
		call("search_news", `{"query":"মিরপুর আগুন"}`),
		call("record_verification", `{"is_real":false,"reason":"প্রমাণ পাইনি"}`),
		both,
	)
	e := Engine{Chat: chat, Tools: verifyTools(s), MaxTurns: 5}
	out := e.Run(context.Background(), "sys", "goal")
	if !out.Finished || out.Tool != "record_verification" || out.Turns != 3 {
		t.Fatalf("a negative verdict must not end the stage, a positive one must: %+v", out)
	}
	if last := e.History[len(e.History)-1]; last.ToolCallID != "late" || !strings.Contains(last.Content, "Not run") {
		t.Fatalf("a call after the final one still needs an answer in the history, got %+v", last)
	}
	for _, tool := range verifyTools(s) {
		if n := tool.Spec.Name; n == "write_draft" || n == "review_draft" || n == "submit_article" {
			t.Fatalf("the verify stage must not offer %s", n)
		}
	}
}

func TestAFailedDraftGoesBackToTheWriterAndThePassedOneToTheReporter(t *testing.T) {
	nr := &fakeNewsroom{}
	chat, seen := scriptedModel(call("submit_article", `{"decision":"publish","note":"যাচাই ও পরীক্ষা শেষ"}`))
	d, sess, s := stagedStory(t, nr, chat)
	ctx := context.Background()

	r := d.stageWrite(ctx, sess)
	if r.Next != StageReview {
		t.Fatalf("after writing comes the review, got %+v", r)
	}
	if r.Source == nil || !strings.Contains(r.Source.Text, "তদন্ত কমিটি") || r.Draft == nil || r.Draft.Text != plainParas(s.written.Body) {
		t.Fatalf("the workflow must get the whole source and the whole draft: %+v", r)
	}
	if u := nr.users["rewrite"]; !strings.Contains(nr.systems["rewrite"]+u, "style") || s.written == nil {
		t.Fatalf("the writer did not write: %s", u)
	}
	if s.review != nil {
		t.Fatal("the writer must not review its own draft in the write stage")
	}
	if r := d.stageReview(ctx, sess); r.Next != StageWrite {
		t.Fatalf("a failed draft goes back to the writer, got %+v", r)
	}

	var good struct{ Body string }
	if err := json.Unmarshal([]byte(goodDraft), &good); err != nil {
		t.Fatal(err)
	}
	nr.reviewerPasses, nr.writerText = true, good.Body
	before := nr.writerCalls
	d.stageWrite(ctx, sess)
	if nr.writerCalls != before+1 || !strings.Contains(nr.users["rewrite"], "আহতদের হাসপাতালের নাম যোগ করো") {
		t.Fatalf("the fix is one writer call that still carries the reporter's notes (%d calls):\n%s", nr.writerCalls-before, nr.users["rewrite"])
	}
	if r := d.stageReview(ctx, sess); r.Next != StageDecide {
		t.Fatalf("a passed draft goes to the reporter, got %+v", r)
	}

	r = d.stageDecide(ctx, sess)
	if len(nr.saved) != 1 || nr.saved[0].ForceReview != "" || sess.published != 1 || sess.run.Processed != 1 || sess.job != nil {
		t.Fatalf("the reporter should have published the story: %+v saved=%+v", r, nr.saved)
	}
	if r.Draft == nil || !strings.HasPrefix(r.Draft.URL, "/article/") || r.Draft.Text == "" {
		t.Fatalf("the decide stage must show the published article in full: %+v", r.Draft)
	}
	if r.Next != StageFinish {
		t.Fatalf("one story per run was asked for, got next=%s", r.Next)
	}
	goal := (*seen)[0][len((*seen)[0])-1].Content
	if !strings.Contains(goal, "The review passed") || !strings.Contains(goal, "নতুন শিরোনাম") {
		t.Fatalf("the reporter must be shown the draft and its review:\n%s", goal)
	}
}

func TestTheWriterLoopLeavesOneReviewForTheReporter(t *testing.T) {
	nr := &fakeNewsroom{}
	chat, seen := scriptedModel(
		call("review_draft", `{"use_written_draft":true,"title":"আগুনে আহত ৪৫"}`),
		call("submit_article", `{"decision":"hold","note":"পরীক্ষা পাস করেনি"}`),
	)
	d, sess, s := stagedStory(t, nr, chat)
	ctx := context.Background()

	next := StageWrite
	for rounds := 0; next != StageDecide; rounds++ {
		if rounds > 2*maxReviews {
			t.Fatal("the write/review loop does not end")
		}
		if next == StageWrite {
			next = d.stageWrite(ctx, sess).Next
		} else {
			next = d.stageReview(ctx, sess).Next
		}
	}
	if s.reviews != maxReviews-1 {
		t.Fatalf("the writer loop used %d reviews, want %d", s.reviews, maxReviews-1)
	}
	d.stageDecide(ctx, sess)
	if s.reviews != maxReviews {
		t.Fatalf("the reporter's own correction was not reviewed (%d reviews)", s.reviews)
	}
	if len(nr.saved) != 1 || nr.saved[0].ForceReview == "" || nr.saved[0].Title != "আগুনে আহত ৪৫" || sess.published != 0 {
		t.Fatalf("a draft that never passed must be held with the reporter's correction: %+v", nr.saved)
	}
	if goal := (*seen)[0][len((*seen)[0])-1].Content; !strings.Contains(goal, "did not pass") {
		t.Fatalf("the reporter must be told the review failed:\n%s", goal)
	}
}

func plainParas(htmlBody string) string { return storyText("", htmlBody).Text }
