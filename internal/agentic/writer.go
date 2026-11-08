package agentic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"bangla-news-aggregator/internal/ai"
)

// The writer is a specialist the reporter agent can call (write_draft). It
// rewrites the source passage by passage, so the result keeps the full
// length and every fact, and no sentence survives word for word — the two
// things a single "rewrite the whole article" call kept getting wrong.

const (
	writerChunkRunes = 1800 // a few paragraphs per writer call
	maxWriterCalls   = 7    // long stories get bigger passages, never a cut-off tail
)

var paraSplit = regexp.MustCompile(`(?i)</p>|<br\s*/?>|\n{2,}`)

// sourcePassages splits the WHOLE source into passages of a few paragraphs
// each. Nothing is dropped: a long story gets bigger passages instead.
func sourcePassages(htmlBody string) []string {
	var paras []string
	total := 0
	for _, p := range paraSplit.Split(htmlBody, -1) {
		if t := plainText(p); utf8.RuneCountInString(t) >= 25 {
			paras = append(paras, t)
			total += utf8.RuneCountInString(t)
		}
	}
	if len(paras) == 0 {
		if t := plainText(htmlBody); t != "" {
			paras = []string{t}
		}
	}
	size := max(writerChunkRunes, total/maxWriterCalls+1)
	var out []string
	var cur strings.Builder
	for _, p := range paras {
		if cur.Len() > 0 && utf8.RuneCountInString(cur.String())+utf8.RuneCountInString(p) > size {
			out = append(out, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(p)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func writerRules(houseStyle, facts string) string {
	return `তুমি "বাংলা নিউজ"-এর একজন অভিজ্ঞ বাংলা সংবাদ লেখক।` + factsBlock(facts) + `
নিয়ম:
১. দেওয়া অংশটি সম্পূর্ণ নিজের ভাষায় নতুন করে লেখো — নতুন শব্দচয়ন, নতুন বাক্যগঠন। কোনো বাক্য হুবহু বা প্রায় হুবহু রাখবে না।
২. প্রতিটি তথ্য, সংখ্যা, নাম, পদবি, তারিখ, স্থান রাখবে। কিছু বাদ দেবে না, সংক্ষেপ করবে না — লেখা মূল অংশের সমান বা একটু বড় হবে।
৩. বক্তৃতা বা বিবৃতি: হয় পরোক্ষ বক্তব্যে নিজের ভাষায় লেখো ("তিনি বলেন, ..." নতুন শব্দে), নয়তো হুবহু অংশটি অবশ্যই উদ্ধৃতিচিহ্ন “…” এর ভেতরে বক্তার নামসহ রাখো। উদ্ধৃতিচিহ্ন ছাড়া মূল লেখার কোনো বাক্য হুবহু রাখা যাবে না — এটি পরীক্ষা করা হয়।
৪. কোনো সংবাদমাধ্যম, সংবাদ সংস্থা, প্রতিবেদক, "নিজস্ব প্রতিবেদক", ওয়েবসাইট, "আরও পড়ুন", ছবির ক্যাপশন বা বিজ্ঞাপন লিখবে না — বাদ দেবে। মূল লেখায় কোনো সংবাদমাধ্যমের নাম থাকলে নাম বাদ দিয়ে সাধারণভাবে লেখো: "নিউইয়র্ক টাইমস জানিয়েছে" → "যুক্তরাষ্ট্রের একটি সংবাদমাধ্যমের খবরে বলা হয়েছে", "রয়টার্সের খবরে" → "আন্তর্জাতিক সংবাদমাধ্যমের খবরে"।
৫. ব্যক্তির পদ (প্রধানমন্ত্রী, মন্ত্রী ইত্যাদি) মূল লেখায় যেমন আছে তেমনই রাখবে — নিজের স্মৃতি দিয়ে "ঠিক" করবে না।
৬. নিজে থেকে কোনো তথ্য, মতামত বা উপসংহার যোগ করবে না।
সম্পাদকীয় স্টাইল: ` + houseStyle + `
উত্তর: শুধু <p>…</p> অনুচ্ছেদে লেখা বাংলা টেক্সট, অন্য কিছু নয়।`
}

// Sources up to this size are fixed in one call after a failed review. A
// fresh rewrite fixes the reported problem but brings new ones (too short,
// other copied lines), so drafts kept failing review until the story was held.
const reviseMaxRunes = 4000

// revisable returns the draft to fix, if the last review failed on it and
// the story is short enough to revise whole.
func (s *story) revisable() *draft {
	if s.draft == nil || s.review == nil || s.review.Passed ||
		utf8.RuneCountInString(plainText(s.item.Content)) > reviseMaxRunes {
		return nil
	}
	return s.draft
}

// revise asks the writer to fix only what the last review found.
func (s *story) revise(ctx context.Context, prev *draft, rules, notes string) (string, error) {
	var problems strings.Builder
	for _, p := range append(append([]string(nil), s.review.Problems...), s.review.Issues...) {
		problems.WriteString("- " + p + "\n")
	}
	for _, c := range s.review.Check.VerbatimCopies {
		problems.WriteString("- মূল লেখা থেকে হুবহু কপি — নতুন শব্দে লেখো, বা বক্তার নামসহ “…” এর ভেতরে রাখো: " + c + "\n")
	}
	user := fmt.Sprintf("মূল খবর (উৎস):\n%s\n\nআমাদের খসড়া:\n%s\n\nসম্পাদক খসড়ায় এই সমস্যাগুলো পেয়েছেন:\n%s%s\n\nশুধু এই সমস্যাগুলো ঠিক করে পুরো খসড়াটি আবার দাও। যে অংশে সমস্যা নেই তা যেমন আছে তেমন রাখো। মূল খবরের কোনো তথ্য বাদ দেবে না — লেখা মূল খবরের সমান বা একটু বড় হবে।",
		plainText(s.item.Content), prev.Body, problems.String(), notes)
	res, err := s.nr.AIText(ctx, ai.GatewayRequest{Task: "rewrite", MaxTokens: 6000, System: rules, User: user})
	if err != nil {
		return "", err
	}
	out := ensureParagraphs(res.Text)
	if plainText(out) == "" {
		return "", errors.New("the writer returned nothing")
	}
	return out, nil
}

func ensureParagraphs(text string) string {
	t := strings.TrimSpace(strings.Trim(strings.TrimSpace(text), "`"))
	t = strings.TrimPrefix(t, "html")
	if strings.Contains(strings.ToLower(t), "<p") {
		return strings.TrimSpace(t)
	}
	var b strings.Builder
	for _, p := range strings.Split(t, "\n") {
		if p = strings.TrimSpace(p); p != "" {
			b.WriteString("<p>" + p + "</p>")
		}
	}
	return b.String()
}

func (s *story) toolWriteDraft(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Notes string `json:"notes"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	passages := sourcePassages(s.item.Content)
	if len(passages) == 0 {
		return "", errors.New("the source has no text to rewrite")
	}
	houseStyle, _ := s.nr.RunSettings()
	rules := writerRules(houseStyle, s.facts)
	notes := ""
	if strings.TrimSpace(a.Notes) != "" {
		notes = "\n\nরিপোর্টারের নির্দেশনা: " + strings.TrimSpace(a.Notes)
	}

	var body strings.Builder
	how := "written passage by passage"
	d := &draft{Title: s.item.Title}
	if prev := s.revisable(); prev != nil {
		if fixed, err := s.revise(ctx, prev, rules, notes); err == nil {
			body.WriteString(fixed)
			how = "revised: only the last review's problems were fixed"
			d.Title, d.Summary, d.Tags = prev.Title, prev.Summary, prev.Tags
		}
	}
	for i, p := range passages {
		if body.Len() > 0 {
			break // revised
		}
		user := fmt.Sprintf("খবরের শিরোনাম (প্রসঙ্গের জন্য): %s\n\nএটি খবরের %d/%d নম্বর অংশ। এই অংশটি নতুন করে লেখো:\n\n%s%s",
			s.item.Title, i+1, len(passages), p, notes)
		if i == 0 {
			user += "\n\nএটি খবরের শুরু: প্রথম অনুচ্ছেদে কে, কী, কোথায়, কখন — মূল খবরটি দুই-তিন বাক্যে দাও।"
		}
		res, err := s.nr.AIText(ctx, ai.GatewayRequest{Task: "rewrite", MaxTokens: 4000, System: rules, User: user})
		if err != nil {
			return "", fmt.Errorf("writer unavailable on passage %d (%v) — try again later or write the draft yourself", i+1, shortErr(err))
		}
		body.WriteString(ensureParagraphs(res.Text))
	}

	d.Body = body.String()
	for try := 0; try < 2 && headlineProblem(s.item.Title, d.Title) != ""; try++ {
		head, err := s.nr.AIText(ctx, ai.GatewayRequest{Task: "rewrite", JSON: true, MaxTokens: 800, System: rules,
			User: fmt.Sprintf("মূল শিরোনাম: %s\n\nনতুন লেখা খবর:\n%s\n\nশুধু JSON দাও: {\"title\":\"নতুন শিরোনাম — একই তথ্য, কিন্তু মূল শিরোনামের শব্দ ও গঠন হুবহু নয়\",\"summary\":\"৩-৪ বাক্যের সারাংশ (plain text)\",\"tags\":[\"৩-৫টি নির্দিষ্ট ট্যাগ\"]}",
				s.item.Title, clip(plainText(body.String()), 2500))})
		if err != nil {
			break
		}
		var h struct {
			Title, Summary string
			Tags           []string
		}
		if json.Unmarshal([]byte(extractJSON(head.Text)), &h) == nil {
			if strings.TrimSpace(h.Title) != "" {
				d.Title = strings.TrimSpace(h.Title)
			}
			d.Summary, d.Tags = strings.TrimSpace(h.Summary), h.Tags
		}
	}
	s.written, s.latest = d, d
	chars := utf8.RuneCountInString(plainText(d.Body))
	orig := utf8.RuneCountInString(plainText(s.item.Content))
	head := fmt.Sprintf("Draft %s: %d characters (%.0f%% of the source).\nTitle: %s\nSummary: %s\nOpening: %s\n",
		how, chars, 100*float64(chars)/float64(max(orig, 1)), d.Title, d.Summary, clip(plainText(d.Body), 300))

	// Review at once, so a good draft goes straight to submit_article.
	v, err := s.runReview(ctx, d, "", nil)
	if err != nil {
		return head + "\nNot reviewed: " + err.Error(), nil
	}
	rv, _ := json.MarshalIndent(v, "", " ")
	return head + "\nREVIEW OF THIS DRAFT:\n" + string(rv), nil
}
