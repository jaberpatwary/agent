package handler

import (
	"strings"

	"bangla-news-aggregator/internal/domain"
)

// A category page of The Daily Star is a run of blocks, each one a
// sub-section (রাজনীতি, অপরাধ ও বিচার, ক্রিকেট…) with its own layout.
// categoryBlocks (blocks_gen.go, made from The Daily Star's pages) lists them;
// the portal has no sub-categories, so a story goes to the block whose
// keywords it matches.

// BlockSpec is one block of a category page.
type BlockSpec struct {
	Key    string // the template that draws it (blocks.html)
	Title  string // the sub-section's name; empty for the opening block
	Layout int    // The Daily Star's layout number
	Cards  int    // how many stories its layout holds
}

// Block is a block filled with stories.
type Block struct {
	Key    string
	Title  string
	Layout int // The Daily Star's layout number
	Href   string
	S      []domain.Article
}

// subsectionKeywords decide which block a story belongs to. A block that is
// not listed here and is not a catch-all gets no stories.
var subsectionKeywords = map[string][]string{
	"রাজনীতি":               {"রাজনীতি", "বিএনপি", "আওয়ামী", "জামায়াত", "সংসদ", "নির্বাচন", "মন্ত্রী", "প্রধানমন্ত্রী", "সরকার", "ভোট", "এমপি", "আন্দোলন", "জাতীয় পার্টি", "হরতাল", "সমাবেশ", "নেতা", "দল"},
	"অপরাধ ও বিচার":         {"হত্যা", "খুন", "গ্রেপ্তার", "মামলা", "আদালত", "রায়", "পুলিশ", "ধর্ষণ", "ডাকাতি", "চাঁদাবাজি", "কারাগার", "দণ্ড", "র‍্যাব", "সন্ত্রাস", "অভিযুক্ত", "জামিন", "যৌন", "নির্যাতন", "প্রতারণা", "অপহরণ"},
	"দুর্ঘটনা ও অগ্নিকাণ্ড": {"দুর্ঘটনা", "আগুন", "অগ্নিকাণ্ড", "সড়ক", "নিহত", "ডুবে", "বজ্রপাত", "বিস্ফোরণ", "ট্রেন", "লঞ্চ", "বাস", "ট্রাক", "আহত", "মৃত্যু", "প্রাণ হারালেন"},
	"বিচিত্র":               {"বিচিত্র", "অদ্ভুত", "রেকর্ড", "বিরল", "চমক", "অবাক", "ভাইরাল", "আশ্চর্য", "মজার", "কাণ্ড"},

	"এশিয়া": {"ভারত", "পাকিস্তান", "চীন", "জাপান", "শ্রীলঙ্কা", "নেপাল", "মিয়ানমার", "আফগানিস্তান", "ইরান", "ইসরায়েল", "ফিলিস্তিন", "গাজা", "সৌদি", "এশিয়া", "থাইল্যান্ড", "ইন্দোনেশিয়া", "ভুটান", "মালদ্বীপ", "কোরিয়া", "তুরস্ক", "লেবানন", "সিরিয়া", "ইরাক", "ইয়েমেন", "দিল্লি", "মুম্বাই", "কাশ্মীর"},
	"বিশ্ব":  {"যুক্তরাষ্ট্র", "মার্কিন", "ট্রাম্প", "রাশিয়া", "ইউক্রেন", "ইউরোপ", "ব্রিটেন", "ফ্রান্স", "জার্মানি", "আফ্রিকা", "কানাডা", "অস্ট্রেলিয়া", "জাতিসংঘ", "নেটো", "বিশ্ব", "ইতালি", "স্পেন", "ব্রাজিল", "আর্জেন্টিনা", "নিউ ইয়র্ক", "লন্ডন", "আমেরিকা", "প্যারিস", "ওয়াশিংটন", "মেক্সিকো"},

	"অভিমত":      {"অভিমত", "মতামত", "ভাবনা", "প্রসঙ্গ", "দৃষ্টিভঙ্গি"},
	"কলাম":       {"কলাম", "লেখা", "স্মরণ", "বিশ্লেষণ"},
	"সম্পাদকীয়": {"সম্পাদকীয়"},

	"ক্রিকেট": {"ক্রিকেট", "টেস্ট", "ওয়ানডে", "টি-টোয়েন্টি", "উইকেট", "ব্যাটার", "বোলার", "বিসিবি", "আইসিসি", "বিপিএল", "সাকিব", "মুশফিক", "ইনিংস", "স্পিনার", "পেসার", "টাইগার", "ওভার"},
	"ফুটবল":   {"ফুটবল", "গোল", "লিগ", "বিশ্বকাপ", "মেসি", "রোনালদো", "বাফুফে", "ফিফা", "বার্সেলোনা", "রিয়াল", "সেমিফাইনাল", "ম্যানচেস্টার", "লিভারপুল", "আর্সেনাল"},

	"অর্থনীতি":       {"অর্থনীতি", "ব্যাংক", "ডলার", "টাকা", "বাজার", "দাম", "বাজেট", "রপ্তানি", "আমদানি", "শেয়ার", "মূল্যস্ফীতি", "বিনিয়োগ", "রিজার্ভ", "ঋণ", "কর", "এনবিআর", "ঋণ", "পণ্য"},
	"বিশ্ব অর্থনীতি": {"মার্কিন", "চীন", "তেলের", "স্বর্ণ", "ফেডারেল", "আইএমএফ", "বিশ্বব্যাংক", "বৈশ্বিক", "আন্তর্জাতিক বাজার", "শুল্ক", "ওপেক", "ইউরো"},
	"সংগঠন সংবাদ":    {"সংগঠন", "চেম্বার", "এফবিসিসিআই", "ব্যবসায়ী", "সমিতি", "বিজিএমইএ", "বিকেএমইএ", "উদ্যোক্তা", "ঢাকা চেম্বার"},

	"টিভি ও সিনেমা": {"সিনেমা", "চলচ্চিত্র", "নাটক", "অভিনেতা", "অভিনেত্রী", "টিভি", "ওয়েব সিরিজ", "পরিচালক", "মুক্তি", "বক্স অফিস", "বলিউড", "হলিউড", "অভিনয়", "ছবি"},
	"মঞ্চ ও সংগীত":  {"গান", "সংগীত", "শিল্পী", "কনসার্ট", "মঞ্চ", "নাট্য", "গায়ক", "গায়িকা", "অ্যালবাম", "ব্যান্ড", "নৃত্য", "গীতিকার"},

	"সুস্থতা":                    {"স্বাস্থ্য", "রোগ", "ডায়াবেটিস", "ঘুম", "ব্যায়াম", "ওজন", "চিকিৎসক", "ভিটামিন", "ক্যানসার", "হৃদ", "চিকিৎসা", "সুস্থ"},
	"সম্পর্ক ও পরিবার":           {"সম্পর্ক", "পরিবার", "বিয়ে", "দাম্পত্য", "মা", "বাবা", "সন্তান", "প্রেম", "বন্ধু", "স্বামী", "স্ত্রী"},
	"পড়াশোনা ও ক্যারিয়ার":      {"ক্যারিয়ার", "চাকরি", "শিক্ষার্থী", "বিশ্ববিদ্যালয়", "ভর্তি", "বৃত্তি", "পরীক্ষা", "দক্ষতা", "শিক্ষা"},
	"ভ্রমণ":                      {"ভ্রমণ", "ঘুরতে", "পর্যটন", "পর্যটক", "সৈকত", "দ্বীপ", "ট্যুর", "সফর"},
	"ফ্যাশন ও সৌন্দর্য":          {"ফ্যাশন", "সৌন্দর্য", "ত্বক", "চুল", "পোশাক", "প্রসাধনী", "মেকআপ", "গয়না"},
	"খাদ্য ও রেসিপি":             {"রেসিপি", "খাবার", "রান্না", "খাদ্য", "পুষ্টি", "মিষ্টি", "ইফতার", "পিঠা", "ফল"},
	"ডায়েরি":                    {"ডায়েরি", "স্মৃতি", "অভিজ্ঞতা"},
	"পোষাপ্রাণী":                 {"পোষা", "বিড়াল", "কুকুর", "প্রাণী", "পাখি"},
	"স্পটলাইট":                   {"স্পটলাইট", "সাক্ষাৎকার", "ব্যক্তিত্ব"},
	"শিশুর যত্ন শিশুর বেড়ে উঠা": {"শিশু", "শিশুর", "নবজাতক", "বাচ্চা"},

	"বিজ্ঞান, প্রযুক্তি, গেজেটস": {"বিজ্ঞান", "গবেষণা", "ফোন", "স্মার্টফোন", "গ্যাজেট", "মহাকাশ", "ল্যাপটপ", "নাসা", "ইসরো", "এআই", "কৃত্রিম বুদ্ধিমত্তা", "চ্যাটজিপিটি", "প্রযুক্তি", "অ্যাপ", "ইন্টারনেট", "সাইবার"},
	"স্টার্টআপ":                  {"স্টার্টআপ", "উদ্যোক্তা", "বিনিয়োগ", "ফান্ডিং", "ইউনিকর্ন", "ডিজিটাল"},
	"অটো মোবাইল":                 {"গাড়ি", "মোটরসাইকেল", "অটো", "ইলেকট্রিক", "ইভি", "টেসলা", "বাইক", "যান"},
}

// minBlockStories is how many stories a matched block is filled up to, so a
// sub-section is never a stub of one or two cards.
const minBlockStories = 5

var bengaliFolds = strings.NewReplacer(
	"য়", "য়", // য় as two letters -> one
	"ড়", "ড়", // ড়
	"ঢ়", "ঢ়", // ঢ়
)

// fold makes spellings of the same Bangla letter compare equal.
func fold(s string) string { return strings.ToLower(bengaliFolds.Replace(s)) }

func isCatchAll(title string) bool { return strings.HasPrefix(title, "অন্যান্য") }

func score(a domain.Article, words []string) int {
	title, body := fold(a.Title), fold(a.Summary)
	n := 0
	for _, w := range words {
		w = fold(w)
		if strings.Contains(title, w) {
			n += 2
		} else if strings.Contains(body, w) {
			n++
		}
	}
	return n
}

// buildBlocks fills a category's blocks from its stories (newest first).
// The opening block takes the newest ones; each sub-section takes the unused
// stories that match its keywords (topped up to a row), the catch-all takes
// what is left, and a sub-section nothing matches is left out.
func buildBlocks(category string, specs []BlockSpec, pool []domain.Article) []Block {
	if len(specs) == 0 || len(pool) == 0 {
		return nil
	}
	used := map[string]bool{}
	take := func(match func(domain.Article) bool, max int) []domain.Article {
		var out []domain.Article
		for _, a := range pool {
			if len(out) >= max {
				break
			}
			id := a.ID.String()
			if !used[id] && match(a) {
				used[id] = true
				out = append(out, a)
			}
		}
		return out
	}
	anyStory := func(domain.Article) bool { return true }
	href := "/?category=" + category

	var blocks []Block
	for i, sp := range specs {
		var stories []domain.Article
		switch {
		case i == 0 || isCatchAll(sp.Title):
			stories = take(anyStory, sp.Cards)
		default:
			words := subsectionKeywords[sp.Title]
			stories = take(func(a domain.Article) bool { return score(a, words) > 0 }, sp.Cards)
			if len(stories) == 0 {
				continue
			}
			if len(stories) < minBlockStories {
				// Fill only with stories no later sub-section wants.
				later := specs[i+1:]
				stories = append(stories, take(func(a domain.Article) bool { return !wantedLater(a, later) }, minBlockStories-len(stories))...)
			}
		}
		if len(stories) == 0 {
			continue
		}
		blocks = append(blocks, Block{Key: sp.Key, Title: sp.Title, Layout: sp.Layout, Href: href, S: stories})
	}
	return blocks
}

// at returns the nth story of a block, or nil when it has fewer, so a
// template draws only the cards it has stories for.
func at(list []domain.Article, i int) *domain.Article {
	if i < 0 || i >= len(list) {
		return nil
	}
	return &list[i]
}

func fields(s string) []string { return strings.Fields(s) }

// wantedLater reports whether a story matches the keywords of a block still to
// be filled.
func wantedLater(a domain.Article, later []BlockSpec) bool {
	for _, sp := range later {
		if words := subsectionKeywords[sp.Title]; len(words) > 0 && score(a, words) > 0 {
			return true
		}
	}
	return false
}
