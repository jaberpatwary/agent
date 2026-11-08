package rss

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func extract(t *testing.T, page string) string {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	return extractArticleText(doc)
}

// Some sites split one story into many body blocks; taking only the biggest
// block published half a story.
func TestStorySplitIntoBlocksIsJoined(t *testing.T) {
	page := `<html><body><div class="story">
<div class="story-element-text"><p>প্রথম অংশ: সকালে নগরীর প্রধান সড়কে দুটি বাসের সংঘর্ষ হয়েছে বলে জানিয়েছে পুলিশ।</p><p>দ্বিতীয় বাক্য: এতে অন্তত দশজন যাত্রী আহত হয়েছেন এবং তাদের হাসপাতালে নেওয়া হয়েছে।</p></div>
<div class="story-element-text"><p>তৃতীয় অংশ: প্রত্যক্ষদর্শীরা বলেছেন, একটি বাস অতিরিক্ত গতিতে চলছিল এবং অন্যটিকে পেছন থেকে ধাক্কা দেয়।</p></div>
<div class="story-element-text"><p>শেষ অংশ: ঘটনার পর প্রায় এক ঘণ্টা সড়কে যান চলাচল বন্ধ ছিল, পরে স্বাভাবিক হয়।</p></div>
</div></body></html>`
	got := extract(t, page)
	for _, part := range []string{"প্রথম অংশ", "তৃতীয় অংশ", "শেষ অংশ"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in %s", part, got)
		}
	}
}

func TestDeclaredArticleBodyIsUsedWhenThePageShowsLess(t *testing.T) {
	full := strings.Repeat("ঘোষিত পূর্ণ খবরের একটি দীর্ঘ বাক্য এখানে রয়েছে। ", 30)
	page := `<html><head><script type="application/ld+json">{"@context":"https://schema.org","@graph":[{"@type":"NewsArticle","headline":"x","articleBody":"` + full + `\nশেষ অনুচ্ছেদটি এখানে।"}]}</script></head>
<body><article><p>পাতায় শুধু প্রথম অনুচ্ছেদটি দেখানো হয়েছে, বাকিটা পরে লোড হয়।</p></article></body></html>`
	got := extract(t, page)
	if !strings.Contains(got, "শেষ অনুচ্ছেদটি এখানে") {
		t.Fatalf("the declared full body should win: %s", got)
	}
}
