package agentic

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Common Bangla words that say nothing about which event a headline is about.
var headlineStopwords = map[string]bool{
	"আছে": true, "করে": true, "করেন": true, "করল": true, "করলেন": true, "বলেন": true, "বললেন": true, "হয়েছে": true,
	"থেকে": true, "জন্য": true, "নিয়ে": true, "দিয়ে": true, "এবং": true, "সঙ্গে": true, "সাথে": true, "বিরুদ্ধে": true,
	"হবে": true, "হলো": true, "হচ্ছে": true, "করা": true, "দিল": true, "দিলেন": true, "কোনো": true, "একটি": true,
	"এক": true, "আর": true, "নয়": true, "নেই": true, "পর": true, "আগে": true, "মধ্যে": true, "দেশের": true,
}

func headlineWords(title string) map[string]bool {
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) || r == '।'
	})
	out := map[string]bool{}
	for _, w := range words {
		if utf8.RuneCountInString(w) >= 3 && !headlineStopwords[w] {
			out[w] = true
		}
	}
	return out
}

// sameWord treats a word and its inflected form as one word: Bangla adds
// short case endings (নভেম্বর/নভেম্বরের, প্রধানমন্ত্রী/প্রধানমন্ত্রীর,
// নয়াপল্টন/নয়াপল্টনে). A longer tail makes a different word (প্রধান /
// প্রধানমন্ত্রী), so it does not count.
func sameWord(a, b string) bool {
	if a == b {
		return true
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	return utf8.RuneCountInString(a) >= 3 && strings.HasPrefix(b, a) && utf8.RuneCountInString(b[len(a):]) <= 3
}

// SameStory reports whether two headlines are very likely about the same
// event: they share at least 3 meaningful words, or (for short headlines)
// at least half of the shorter one's words and no fewer than 2.
func SameStory(a, b string) bool {
	wa, wb := headlineWords(a), headlineWords(b)
	shared := 0
	for w := range wa {
		for x := range wb {
			if sameWord(w, x) {
				shared++
				break
			}
		}
	}
	smaller := min(len(wa), len(wb))
	if shared >= 3 {
		return true
	}
	return smaller > 0 && shared >= 2 && float64(shared)/float64(smaller) >= 0.5
}
