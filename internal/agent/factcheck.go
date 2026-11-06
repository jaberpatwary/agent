package agent

import (
	"regexp"
	"strings"
)

// FactCheckResult is a cheap, local, code-based safety net that runs after
// the LLM's own self-review step. It is NOT a substitute for that step — it
// cannot tell whether a sentence is true — but it reliably catches the
// single most damaging and most common failure mode of AI news rewrites: a
// significant number (a death toll, a price, a percentage, a date) that got
// dropped or silently changed while the sentence around it was being
// reworded.
type FactCheckResult struct {
	Consistent     bool
	MissingNumbers []string // significant numbers present in the source but absent from the rewrite
}

var (
	banglaDigitReplacer = strings.NewReplacer(
		"০", "0", "১", "1", "২", "2", "৩", "3", "৪", "4",
		"৫", "5", "৬", "6", "৭", "7", "৮", "8", "৯", "9",
	)
	// Matches runs of 2+ digits, optionally with a decimal point, a trailing
	// percent sign, or a comma as a thousands separator (৩,০০০ / 3,000).
	numberRegex = regexp.MustCompile(`\d[\d,]*(\.\d+)?%?`)
	// Machine timestamps scraped along with the article ("2026-11-06 20:15",
	// "06/11/2026", "20:15") are page furniture, not facts a rewrite must repeat.
	timestampRegex = regexp.MustCompile(`\d{4}-\d{1,2}-\d{1,2}([ T]\d{1,2}:\d{2}(:\d{2})?)?|\d{1,2}/\d{1,2}/\d{2,4}|\b\d{1,2}:\d{2}(:\d{2})?\b`)
)

// CheckFactConsistency compares the significant numbers mentioned in the
// original source text(s) against the rewritten body. sourceTexts may be one
// article (single-source rewrite) or several (multi-source synthesis).
func CheckFactConsistency(sourceTexts []string, rewrittenBody string) FactCheckResult {
	source := strings.Join(sourceTexts, " ")
	sourceNums := significantNumbers(source)
	if len(sourceNums) == 0 {
		return FactCheckResult{Consistent: true}
	}

	rewriteSet := make(map[string]bool, len(sourceNums))
	for _, n := range significantNumbers(rewrittenBody) {
		rewriteSet[n] = true
	}

	seen := make(map[string]bool)
	var missing []string
	for _, n := range sourceNums {
		if rewriteSet[n] || seen[n] {
			continue
		}
		seen[n] = true
		missing = append(missing, n)
	}

	// Only flag when a meaningful share of the source's own numbers failed
	// to carry over — a single missing figure out of many (e.g. a page
	// number, a minor rounding choice) is normal and not worth a human
	// review queue; losing most of them usually means the rewrite drifted
	// from the source.
	consistent := true
	if len(sourceNums) >= 2 {
		missingRatio := float64(len(missing)) / float64(len(sourceNums))
		if missingRatio > 0.5 {
			consistent = false
		}
	}

	return FactCheckResult{Consistent: consistent, MissingNumbers: missing}
}

// significantNumbers extracts normalized numeric tokens (Bangla digits
// converted to Latin, thousands-separator commas kept) that are at least two
// digits long, so page numbers, bullet markers, and other single-digit noise
// don't pollute the comparison.
func significantNumbers(text string) []string {
	normalized := timestampRegex.ReplaceAllString(banglaDigitReplacer.Replace(text), " ")
	matches := numberRegex.FindAllString(normalized, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		digitsOnly := strings.Count(m, "0") + strings.Count(m, "1") + strings.Count(m, "2") +
			strings.Count(m, "3") + strings.Count(m, "4") + strings.Count(m, "5") +
			strings.Count(m, "6") + strings.Count(m, "7") + strings.Count(m, "8") + strings.Count(m, "9")
		if digitsOnly >= 2 {
			out = append(out, m)
		}
	}
	return out
}
