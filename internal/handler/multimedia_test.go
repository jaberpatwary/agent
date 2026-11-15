package handler

import (
	"bangla-news-aggregator/internal/domain"
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

const multimediaHTML = `<html><body>
<div class="news-section news-section-layout-3"><h2 class="section-title">সংস্কৃতি</h2><div class="card"><h3 class="card-title"><a href="/x">অন্য</a></h3></div></div>
<div class="news-section news-section-layout-30"><h2 class="section-title"><a href="/star-multimedia"> স্টার মাল্টিমিডিয়া </a></h2>
 <div class="card"><div class="card-image"><a href="/star-multimedia/news-1"><img src="/sites/a.jpeg?h=1" alt="a"></a></div>
  <div class="card-content"><h3 class="card-title"><a href="/star-multimedia/news-1">
     প্রথম  ভিডিও </a></h3>
  <div class="card-info"><span><time datetime="2026-09-15T19:50:28+06:00">১৫ সেপ্টেম্বর ২০২৬</time></span><span class="flex items-center"><span class="rounded-full"></span> স্টার মাল্টিমিডিয়া</span></div></div></div>
 <div class="card"><div class="card-image"><img src="https://cdn.example/b.jpg"></div>
  <div class="card-content"><h5 class="card-title"><a href="/star-multimedia/sports-multimedia/news-2">দ্বিতীয়</a></h5></div></div>
 <div class="card"><h5 class="card-title"><a href="/star-multimedia/news-3">ছবি নেই</a></h5></div>
</div></body></html>`

func TestParseMultimediaReadsOnlyThatSection(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(multimediaHTML))
	if err != nil {
		t.Fatal(err)
	}
	items := parseMultimedia(doc)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (the section's other block and the card without a photo are skipped): %+v", len(items), items)
	}
	first := items[0]
	if first.Title != "প্রথম ভিডিও" ||
		first.URL != "https://bangla.thedailystar.net/star-multimedia/news-1" ||
		first.Image != "https://bangla.thedailystar.net/sites/a.jpeg?h=1" ||
		first.When != "১৫ সেপ্টেম্বর ২০২৬" || first.Label != "স্টার মাল্টিমিডিয়া" {
		t.Fatalf("first card = %+v", first)
	}
	if items[1].Image != "https://cdn.example/b.jpg" {
		t.Fatalf("absolute image address was changed: %s", items[1].Image)
	}
}

func TestMultimediaKeepsLastGoodCardsWhenTheSourceFails(t *testing.T) {
	calls := 0
	c := &multimediaCache{fetch: func(context.Context) ([]MultimediaItem, error) {
		calls++
		if calls == 1 {
			return []MultimediaItem{{Title: "ভিডিও"}}, nil
		}
		return nil, context.DeadlineExceeded
	}}
	if got := c.get(); len(got) != 1 {
		t.Fatalf("first fetch gave %d items", len(got))
	}
	// Expire the cache: a failing refresh must not drop what was shown.
	c.fetched = c.fetched.Add(-2 * multimediaTTL)
	if got := c.get(); len(got) != 1 || calls != 2 {
		t.Fatalf("after a failed refresh: %d items, %d fetches", len(got), calls)
	}
}

func TestFrontPageShowsTheMultimediaSection(t *testing.T) {
	cats := map[string][]domain.Article{}
	for _, k := range homeCategories {
		cats[k] = testArticles(k, 14)
	}
	videos := make([]MultimediaItem, 7)
	for i := range videos {
		videos[i] = MultimediaItem{Title: "ভিডিও " + string(rune('ক'+i)), URL: "https://bangla.thedailystar.net/star-multimedia/news-1",
			Image: "https://bangla.thedailystar.net/a.jpg", When: "১৫ সেপ্টেম্বর ২০২৬", Label: "স্টার মাল্টিমিডিয়া"}
	}
	out := render(t, "index.html", map[string]any{"Front": true, "Cat": cats, "Multimedia": videos, "Articles": cats["bangladesh"], "MostRead": ranked(testArticles("sports", 5)), "Page": 1})
	if !strings.Contains(out, "স্টার মাল্টিমিডিয়া") {
		t.Fatal("the section title is missing")
	}
	if n := strings.Count(out, `aria-label="ভিডিও `); n != 7 {
		t.Fatalf("%d video cards, want 7", n)
	}
	// With no videos the section is left out rather than shown empty.
	bare := render(t, "index.html", map[string]any{"Front": true, "Cat": cats, "Articles": cats["bangladesh"], "MostRead": ranked(testArticles("sports", 5)), "Page": 1})
	if strings.Contains(bare, "star-multimedia") {
		t.Fatal("an empty multimedia section was rendered")
	}
}
