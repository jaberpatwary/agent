package agentic

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"bangla-news-aggregator/internal/cron"
)

// Hard quality rules every draft must pass before it can be published.
// They are plain code, not AI, so the agent cannot talk its way past them.
const (
	minLengthRatio   = 0.85 // body must keep at least this share of the original's text
	minBodyChars     = 600
	maxVerbatimLines = 1 // sentences copied word for word from the source
	// Shorter sentences are stock phrases ("বর্তমানে পরিস্থিতি স্বাভাবিক
	// রয়েছে"); repeating one is not copying the story.
	minVerbatimWords = 7
)

var (
	tagRe      = regexp.MustCompile(`<[^>]*>`)
	sentenceRe = regexp.MustCompile(`[^।!?\n]+`)
)

// Outlet names that must never appear in a published story (the portal
// does not reveal where a story came from). Feed names are added at runtime.
var knownOutlets = []string{
	"প্রথম আলো", "prothom alo", "prothomalo", "যুগান্তর", "jugantor", "কালের কণ্ঠ", "kaler kantho", "সমকাল", "samakal",
	"ইত্তেফাক", "ittefaq", "জাগো নিউজ", "জাগোনিউজ", "jagonews", "বাংলা ট্রিবিউন", "bangla tribune", "ঢাকা পোস্ট", "dhaka post",
	"ঢাকা মেইল", "dhaka mail", "সময় নিউজ", "somoy news", "রাইজিংবিডি", "risingbd", "বিডিনিউজ", "bdnews24", "ডেইলি স্টার",
	"daily star", "আজকের পত্রিকা", "নয়া দিগন্ত", "মানবজমিন", "বাংলানিউজ২৪", "banglanews24", "ডেইলি বাংলাদেশ", "daily bangladesh",
	"bd24live", "techshohor", "টেকশহর", "নিজস্ব প্রতিবেদক", "স্টাফ রিপোর্টার", "আরও পড়ুন", ".com",
}

// DraftCheck is the result of the code checks on a draft.
type DraftCheck struct {
	BodyChars      int      `json:"body_chars"`
	OriginalChars  int      `json:"original_chars"`
	LengthRatio    float64  `json:"length_ratio"`
	VerbatimCopies []string `json:"verbatim_copies,omitempty"`
	OutletMentions []string `json:"outlet_mentions,omitempty"`
	MissingNumbers []string `json:"missing_numbers,omitempty"`
	Problems       []string `json:"problems,omitempty"`
}

// OK reports whether the draft passed every hard rule.
func (c DraftCheck) OK() bool { return len(c.Problems) == 0 }

// headlineProblem flags a headline that is the source's own (ignoring
// punctuation and spacing): the body is rewritten, the headline must be too.
func headlineProblem(sourceTitle, draftTitle string) string {
	norm := func(s string) string {
		return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsPunct(r) || r == '।' || r == '‘' || r == '’'
		}), " ")
	}
	if a, b := norm(sourceTitle), norm(draftTitle); a != "" && a == b {
		return "the headline is the source's headline word for word — write a new headline in our own words (same facts)"
	}
	return ""
}

func plainText(htmlBody string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(htmlBody, " "))), " ")
}

// CheckDraft runs the hard rules on a draft against the original story.
// ignoreNumbers are figures the reporter says are page furniture (times,
// menus, other headlines) rather than facts of the story; only figures that
// really occur in the source can be ignored, and the AI editor double-checks.
func CheckDraft(originalTitle, originalBody, draftBody string, extraOutlets, ignoreNumbers []string) DraftCheck {
	orig := plainText(originalBody)
	body := plainText(draftBody)
	c := DraftCheck{BodyChars: utf8.RuneCountInString(body), OriginalChars: utf8.RuneCountInString(orig)}
	if c.OriginalChars > 0 {
		c.LengthRatio = float64(c.BodyChars) / float64(c.OriginalChars)
	}

	if !strings.Contains(strings.ToLower(draftBody), "<p") {
		c.Problems = append(c.Problems, "body must be HTML paragraphs (<p>…</p>)")
	}
	floor := minBodyChars
	if limit := int(float64(c.OriginalChars) * minLengthRatio); c.OriginalChars > 0 && limit < floor {
		floor = limit // a very short source can't require more than its own length
	}
	if c.BodyChars < floor {
		c.Problems = append(c.Problems, fmt.Sprintf("body too short: %d characters (minimum %d)", c.BodyChars, floor))
	}
	if c.OriginalChars > 0 && c.LengthRatio < minLengthRatio {
		c.Problems = append(c.Problems, fmt.Sprintf("not the full story: %d characters is %.0f%% of the original's %d — add the missing facts and details (need at least %.0f%%)",
			c.BodyChars, c.LengthRatio*100, c.OriginalChars, minLengthRatio*100))
	}

	lowerBody := strings.ToLower(body)
	eligible := 0
	for _, s := range sentenceRe.FindAllString(orig, -1) {
		s = strings.TrimSpace(s)
		if len(strings.Fields(s)) < minVerbatimWords || strings.ContainsAny(s, "\"“”'‘’") {
			continue // short stock lines and direct quotes may legitimately repeat
		}
		eligible++
		if i := strings.Index(lowerBody, strings.ToLower(s)); i >= 0 && !quotedAt(lowerBody, i) {
			c.VerbatimCopies = append(c.VerbatimCopies, s)
		}
	}
	// Long speeches reported without quote marks can't always be reworded line
	// by line, so allow up to 10% of the source's sentences (at least one).
	allowed := max(maxVerbatimLines, eligible/10)
	if len(c.VerbatimCopies) > allowed {
		c.Problems = append(c.Problems, fmt.Sprintf("%d sentences are copied word for word from the source (allowed %d) — rewrite them in your own words", len(c.VerbatimCopies), allowed))
	}

	seen := map[string]bool{}
	for _, name := range append(append([]string(nil), knownOutlets...), extraOutlets...) {
		n := strings.ToLower(strings.TrimSpace(name))
		if utf8.RuneCountInString(n) < 3 || seen[n] || n == "বাংলা নিউজ" {
			continue
		}
		seen[n] = true
		if strings.Contains(lowerBody, n) {
			c.OutletMentions = append(c.OutletMentions, name)
		}
	}
	if len(c.OutletMentions) > 0 {
		c.Problems = append(c.Problems, "mentions a news outlet, reporter credit or link ("+strings.Join(c.OutletMentions, ", ")+") — the portal never names its sources; attribute facts to people or institutions instead")
	}

	fc := cron.FactCheckNumbers([]string{originalTitle, originalBody}, draftBody)
	if !fc.Consistent {
		normalizedSource := bnDigits.Replace(originalTitle + " " + orig)
		skip := map[string]bool{}
		for _, n := range ignoreNumbers {
			n = bnDigits.Replace(strings.TrimSpace(n))
			if n != "" && strings.Contains(normalizedSource, n) {
				skip[n] = true
			}
		}
		for _, n := range fc.MissingNumbers {
			if !skip[n] {
				c.MissingNumbers = append(c.MissingNumbers, n)
			}
		}
		if len(c.MissingNumbers) > 0 {
			c.Problems = append(c.Problems, "important numbers from the original are missing or changed: "+strings.Join(c.MissingNumbers, ", ")+
				" — put them in the story, or if they are NOT facts of this story (times, menu items, other headlines) list them in numbers_not_in_story")
		}
	}
	return c
}

var bnDigits = strings.NewReplacer("০", "0", "১", "1", "২", "2", "৩", "3", "৪", "4", "৫", "5", "৬", "6", "৭", "7", "৮", "8", "৯", "9")

// quotedAt reports whether the text at byte offset i sits right after an
// opening quotation mark — a direct quote, which may repeat a speaker's words.
func quotedAt(text string, i int) bool {
	start := max(0, i-8)
	before := strings.TrimRight(text[start:i], " ")
	return strings.HasSuffix(before, "\"") || strings.HasSuffix(before, "“") || strings.HasSuffix(before, "‘") || strings.HasSuffix(before, "'")
}
