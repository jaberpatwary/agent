package handler

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"bangla-news-aggregator/internal/domain"
)

func story(title string, age int) domain.Article {
	return domain.Article{ID: uuid.New(), Title: title, Summary: "সারাংশ", ImageURL: "/img/x.jpg",
		PublishedAt: time.Now().Add(-time.Duration(age) * time.Hour), Status: "published"}
}

func TestBlocksGiveEachSubsectionTheStoriesThatMatchIt(t *testing.T) {
	var pool []domain.Article
	for i := 0; i < 4; i++ {
		pool = append(pool, story(fmt.Sprintf("সাধারণ খবর %d", i), i))
	}
	pool = append(pool,
		story("সংসদে বিএনপির নেতা বক্তব্য দিলেন", 5),
		story("আদালতে খুনের মামলার রায়", 6),
		story("সড়ক দুর্ঘটনায় ৩ জন নিহত", 7),
	)
	specs := []BlockSpec{
		{Key: "k0", Layout: 8, Cards: 4},
		{Key: "k1", Title: "রাজনীতি", Layout: 15, Cards: 10},
		{Key: "k2", Title: "অপরাধ ও বিচার", Layout: 24, Cards: 10},
		{Key: "k3", Title: "বিচিত্র", Layout: 16, Cards: 7}, // nothing matches: left out
		{Key: "k4", Title: "অন্যান্য", Layout: 31, Cards: 5},
	}
	blocks := buildBlocks("bangladesh", specs, pool)

	var keys []string
	for _, b := range blocks {
		keys = append(keys, b.Key)
	}
	if strings.Join(keys, ",") != "k0,k1,k2" {
		t.Fatalf("blocks = %v, want k0,k1,k2 (বিচিত্র has no match and অন্যান্য no stories left)", keys)
	}
	if blocks[0].Layout != 8 || len(blocks[0].S) != 4 || blocks[0].Title != "" {
		t.Fatalf("opening block = %+v", blocks[0])
	}
	if !strings.Contains(blocks[1].S[0].Title, "বিএনপি") {
		t.Fatalf("রাজনীতি opens with %q", blocks[1].S[0].Title)
	}
	seen := map[string]bool{}
	for _, b := range blocks {
		for _, s := range b.S {
			if seen[s.ID.String()] {
				t.Fatalf("%q appears in two blocks", s.Title)
			}
			seen[s.ID.String()] = true
		}
	}
}

func TestSubsectionIsToppedUpToARowButNeverBeyond(t *testing.T) {
	var pool []domain.Article
	for i := 0; i < 20; i++ {
		pool = append(pool, story(fmt.Sprintf("খবর %d", i), i))
	}
	pool = append(pool, story("সাফ ফুটবলে বাংলাদেশের জয়", 30))
	blocks := buildBlocks("sports", []BlockSpec{{Key: "a", Cards: 2}, {Key: "b", Title: "ফুটবল", Cards: 10}}, pool)
	if len(blocks) != 2 || len(blocks[1].S) != minBlockStories {
		t.Fatalf("a one-story match must be filled to %d cards, got %+v", minBlockStories, blocks)
	}
}

func TestSpellingsOfTheSameLetterMatch(t *testing.T) {
	// "য়" typed as two letters in a keyword still matches the single letter.
	a := story("পরিবারের আয়োজন", 0)
	if score(a, []string{"আয়োজন"}) == 0 {
		t.Fatal("য় written as য + ় did not match য়")
	}
}

func TestEveryGeneratedBlockHasATemplate(t *testing.T) {
	for cat, specs := range categoryBlocks {
		for _, sp := range specs {
			out := render(t, "cat_block", Block{Key: sp.Key, Title: sp.Title, S: testArticles(cat, sp.Cards)})
			if !strings.Contains(out, fmt.Sprintf("news-section-layout-%d", sp.Layout)) {
				t.Errorf("%s (%s) did not draw layout %d", sp.Key, cat, sp.Layout)
			}
		}
	}
}

func TestCategoryPageDrawsItsBlocksAndTheTabBox(t *testing.T) {
	pool := testArticles("bangladesh", 60)
	blocks := buildBlocks("bangladesh", categoryBlocks["bangladesh"], pool)
	out := render(t, "index.html", map[string]any{"Category": "bangladesh", "Page": 1, "Articles": pool[:20], "Blocks": blocks,
		"TabBeside": true, "TabLatest": pool[:5], "MostRead": ranked(testArticles("sports", 5))})
	for _, want := range []string{"news-section-layout-8", "most-popular-tabs"} {
		if !strings.Contains(out, want) {
			t.Errorf("category page lacks %q", want)
		}
	}
}
