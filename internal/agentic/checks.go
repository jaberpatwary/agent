package agentic

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	"bangla-news-aggregator/internal/cron"
)

// Hard quality rules every draft must pass before it can be published.
// They are plain code, not AI, so the agent cannot talk its way past them.
const (
	minLengthRatio   = 0.8 // body must keep at least this share of the original's text
	minBodyChars     = 600
	maxVerbatimLines = 1 // sentences copied word for word from the source
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

func plainText(htmlBody string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(htmlBody, " "))), " ")
}

// CheckDraft runs the hard rules on a draft against the original story.
func CheckDraft(originalTitle, originalBody, draftBody string, extraOutlets []string) DraftCheck {
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
	for _, s := range sentenceRe.FindAllString(orig, -1) {
		s = strings.TrimSpace(s)
		if utf8.RuneCountInString(s) < 30 || strings.ContainsAny(s, "\"“”'‘’") {
			continue // short fragments and direct quotes may legitimately repeat
		}
		if strings.Contains(lowerBody, strings.ToLower(s)) {
			c.VerbatimCopies = append(c.VerbatimCopies, s)
		}
	}
	if len(c.VerbatimCopies) > maxVerbatimLines {
		c.Problems = append(c.Problems, fmt.Sprintf("%d sentences are copied word for word from the source — rewrite them in your own words", len(c.VerbatimCopies)))
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
		c.MissingNumbers = fc.MissingNumbers
		c.Problems = append(c.Problems, "important numbers from the original are missing or changed: "+strings.Join(fc.MissingNumbers, ", "))
	}
	return c
}
