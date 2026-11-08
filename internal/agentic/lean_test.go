package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"bangla-news-aggregator/internal/ai"
)

// The desk must be able to finish a story in two model turns:
// (record_verification + write_draft) then submit_article.
func TestWriteDraftReviewsItsDraftSoTheAgentCanSubmitNext(t *testing.T) {
	var body struct{ Body string }
	_ = json.Unmarshal([]byte(goodDraft), &body)
	nr := &fakeNewsroom{reviewerPasses: true, writerText: body.Body}
	s, tools, _ := newTestStory(nr, true)

	gathered := s.observe(context.Background(), func(StepRecord) {})
	if !strings.Contains(gathered, "search_news") || !strings.Contains(gathered, "Other Outlet") {
		t.Fatalf("the desk should gather news coverage before the first turn: %s", gathered)
	}
	// The gathered search counts as evidence, so the agent can verify at once.
	mustRun(t, tools, "record_verification", `{"is_real":true,"reason":"অন্য সংবাদমাধ্যমেও এসেছে"}`)
	out := mustRun(t, tools, "write_draft", `{}`)
	if !strings.Contains(out, "REVIEW OF THIS DRAFT") || !strings.Contains(out, `"passed": true`) {
		t.Fatalf("write_draft should review its own draft: %s", out)
	}
	if _, err := run(t, tools, "submit_article", `{"decision":"publish","note":"যাচাই করা হয়েছে"}`); err != nil {
		t.Fatalf("a reviewed, passed draft should publish without another review call: %v", err)
	}
	if len(nr.saved) != 1 || nr.saved[0].Title == s.item.Title {
		t.Fatalf("published with a new headline expected, got %+v", nr.saved)
	}
}

func TestTheSourceHeadlineIsNotANewHeadline(t *testing.T) {
	nr := &fakeNewsroom{reviewerPasses: true}
	_, tools, _ := newTestStory(nr, true)
	var d map[string]any
	_ = json.Unmarshal([]byte(goodDraft), &d)
	d["title"] = " মিরপুরে  আগুন। " // the source's headline, different spacing/punctuation
	raw, _ := json.Marshal(d)
	out := mustRun(t, tools, "review_draft", string(raw))
	if !strings.Contains(out, "headline") || strings.Contains(out, `"passed": true`) {
		t.Fatalf("a copied headline must fail the review: %s", out)
	}
	if p := headlineProblem("মিরপুরে আগুন", "মিরপুরের কারখানায় আগুন, আহত ৪৫"); p != "" {
		t.Fatalf("a new headline must pass: %s", p)
	}
}

func TestLongStoriesAreRewrittenWhole(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "<p>অনুচ্ছেদ %d: %s</p>", i, strings.Repeat("এখানে খবরের বিস্তারিত তথ্য রয়েছে। ", 10))
	}
	passages := sourcePassages(b.String())
	if len(passages) > maxWriterCalls+1 {
		t.Fatalf("a long story should use at most about %d writer calls, got %d", maxWriterCalls, len(passages))
	}
	joined := strings.Join(passages, " ")
	if !strings.Contains(joined, "অনুচ্ছেদ 40:") {
		t.Fatal("the end of a long story must not be cut off")
	}
	if got, want := utf8.RuneCountInString(joined), utf8.RuneCountInString(plainText(b.String())); got < want*95/100 {
		t.Fatalf("passages lost text: %d of %d runes", got, want)
	}
}

func TestOldToolResultsAreShortenedOnceSeen(t *testing.T) {
	long := strings.Repeat("ফলাফল ", 400)
	msgs := []ai.ChatMessage{
		{Role: "user", Content: "goal " + long},
		{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "1", Name: "search_news"}}},
		{Role: "tool", ToolCallID: "1", ToolName: "search_news", Content: long},
		{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "2", Name: "write_draft"}}},
		{Role: "tool", ToolCallID: "2", ToolName: "write_draft", Content: long},
	}
	got := compactHistory(msgs)
	if utf8.RuneCountInString(got[2].Content) > seenToolRunes+100 {
		t.Fatal("a result the model already read should be shortened")
	}
	if got[4].Content != long || got[0].Content != msgs[0].Content {
		t.Fatal("the goal and the newest result must stay complete")
	}
	if msgs[2].Content != long {
		t.Fatal("the full history must stay intact for the trace")
	}
}

// The reviewer judges today's office-holders by the editors' current facts,
// not by its outdated memory; and no prompt shows the outlets behind a fact.
func TestReviewerKnowsTheCurrentFacts(t *testing.T) {
	nr := &fakeNewsroom{reviewerPasses: true}
	s, tools, _ := newTestStory(nr, true)
	s.facts = "তারেক রহমান বাংলাদেশের বর্তমান প্রধানমন্ত্রী (বিএনপি সরকার) — সূত্র: Outlet A, Outlet B (2026-11-06)"
	mustRun(t, tools, "write_draft", `{}`)
	for _, task := range []string{"review", "rewrite"} {
		sys := nr.systems[task]
		if !strings.Contains(sys, "তারেক রহমান বাংলাদেশের বর্তমান প্রধানমন্ত্রী") {
			t.Fatalf("%s prompt lacks the current facts:\n%s", task, sys)
		}
		if strings.Contains(sys, "Outlet A") || strings.Contains(sys, "সূত্র:") {
			t.Fatalf("%s prompt names the outlets behind a fact:\n%s", task, sys)
		}
	}
}

// After a failed review the writer fixes the reported problems in the draft
// instead of starting over, and a held story gives the editor the newest draft.
func TestFailedReviewIsFixedNotRewrittenAndHoldKeepsTheNewestDraft(t *testing.T) {
	nr := &fakeNewsroom{}
	s, tools, _ := newTestStory(nr, true)
	mustRun(t, tools, "search_news", `{"query":"মিরপুর আগুন"}`)
	mustRun(t, tools, "record_verification", `{"is_real":true,"reason":"x","corroborating_outlets":["Other Outlet"]}`)
	mustRun(t, tools, "write_draft", `{}`)
	first := nr.writerCalls
	nr.writerText = "<p>ঠিক করা খসড়া " + strings.Repeat("নতুন লেখা ", 40) + "</p>"
	out := mustRun(t, tools, "write_draft", `{"notes":"আহতের সংখ্যা যোগ করো"}`)
	if nr.writerCalls != first+1 {
		t.Fatalf("a revision is one writer call, got %d", nr.writerCalls-first)
	}
	u := nr.users["rewrite"]
	if !strings.Contains(u, "আমাদের খসড়া") || !strings.Contains(u, "৪৫ জন আহতের তথ্য নেই") || !strings.Contains(u, "আহতের সংখ্যা যোগ করো") {
		t.Fatalf("the writer must get the draft, the review's problems and the notes:\n%s", u)
	}
	if !strings.Contains(out, "revised") {
		t.Fatalf("result should say the draft was revised: %s", out)
	}
	// Use up the reviews; the last draft is written but not reviewed.
	for s.reviews < maxReviews {
		mustRun(t, tools, "write_draft", `{}`)
	}
	nr.writerText = "<p>সবশেষ খসড়া " + strings.Repeat("নতুন লেখা ", 40) + "</p>"
	mustRun(t, tools, "write_draft", `{}`)
	mustRun(t, tools, "submit_article", `{"decision":"hold","note":"রিভিউ পাস করেনি"}`)
	if got := nr.saved[0].Body; !strings.Contains(got, "সবশেষ খসড়া") {
		t.Fatalf("the held story must carry the newest draft, got %q", got)
	}
}

func TestShortStockLinesAreNotCopying(t *testing.T) {
	src := `<p>পুলিশ ঘটনাস্থলে গিয়ে পরিস্থিতি নিয়ন্ত্রণে আনে। বর্তমানে পরিস্থিতি স্বাভাবিক রয়েছে। অভিযোগ পেলে আইনগত ব্যবস্থা নেওয়া হবে।</p>
<p>এ নিয়ে দুই পক্ষের নেতাকর্মীদের মধ্যে প্রথমে বাকবিতণ্ডা ও উত্তেজনা সৃষ্টি হয়। পরে দুই পক্ষ ইটপাটকেল ও লাঠিসোঁটা নিয়ে সংঘর্ষে জড়িয়ে পড়ে।</p>`
	draft := `<p>খবর পেয়ে পুলিশ গিয়ে অবস্থা সামাল দেয়। বর্তমানে পরিস্থিতি স্বাভাবিক রয়েছে। অভিযোগ পেলে আইনগত ব্যবস্থা নেওয়া হবে।</p>
<p>এ নিয়ে দুই পক্ষের নেতাকর্মীদের মধ্যে প্রথমে বাকবিতণ্ডা ও উত্তেজনা সৃষ্টি হয়। এরপর ইট ও লাঠি হাতে দুই দল একে অপরের ওপর চড়াও হয়।</p>`
	c := CheckDraft("", src, draft, nil, nil)
	if len(c.VerbatimCopies) != 1 || !strings.Contains(c.VerbatimCopies[0], "বাকবিতণ্ডা") {
		t.Fatalf("only the long copied sentence counts, got %q", c.VerbatimCopies)
	}
}
