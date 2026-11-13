package handler

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"bangla-news-aggregator/internal/domain"
)

func testArticles(cat string, n int) []domain.Article {
	out := make([]domain.Article, n)
	for i := range out {
		out[i] = domain.Article{ID: uuid.New(), Title: fmt.Sprintf("%s শিরোনাম %d", cat, i), Summary: "সারাংশ <b>লেখা</b>",
			Content: "<p>প্রথম অনুচ্ছেদ</p><h3>উপশিরোনাম</h3><p>দ্বিতীয়</p>", ImageURL: "/img/x.jpg", Category: cat,
			PublishedAt: time.Now().Add(-time.Duration(i) * time.Hour), Status: "published"}
	}
	return out
}

func render(t *testing.T, name string, data any) string {
	t.Helper()
	tpl := template.Must(template.New("").Funcs(templateFuncs()).ParseGlob("../../web/templates/*/*.html"))
	var b bytes.Buffer
	if err := tpl.ExecuteTemplate(&b, name, data); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return b.String()
}

func TestFrontPageFillsEveryDailyStarSection(t *testing.T) {
	cats := map[string][]domain.Article{}
	for _, k := range homeCategories {
		cats[k] = testArticles(k, 14)
	}
	html := render(t, "index.html", gin.H{"Front": true, "Cat": cats, "MostRead": ranked(testArticles("sports", 5)),
		"HeaderNews": testArticles("bangladesh", 3), "Articles": testArticles("bangladesh", 20), "Page": 1})
	for _, want := range []string{"news-section-layout-35", "news-section-layout-36", "most-popular-layout-2", "news-section-layout-9",
		"news-section-layout-3", "news-section-layout-31", "header-news-card", "/static/ds/logo.png", "সর্বাধিক পঠিত",
		"technology শিরোনাম 4", "international শিরোনাম 9", "general শিরোনাম 9", "bangladesh শিরোনাম 12"} {
		if !strings.Contains(html, want) {
			t.Errorf("front page lacks %q", want)
		}
	}
	if strings.Contains(html, "<b>") {
		t.Error("summaries must be shown as plain text")
	}
}

func TestFrontPageWithFewStoriesStillRenders(t *testing.T) {
	render(t, "index.html", gin.H{"Front": true, "Cat": map[string][]domain.Article{"sports": testArticles("sports", 1)},
		"Articles": testArticles("bangladesh", 3), "Page": 1})
	render(t, "index.html", gin.H{"Front": true, "Cat": map[string][]domain.Article{}, "Page": 1})
}

func TestSectionPageShowsEveryStoryOnce(t *testing.T) {
	for _, n := range []int{1, 5, 12, 20} {
		list := testArticles("sports", n)
		html := render(t, "index.html", gin.H{"Category": "sports", "Articles": list, "Page": 1, "HasMore": true})
		for _, a := range list {
			if c := strings.Count(html, `<a href="/article/`+a.ID.String()+`">`); c != 1 {
				t.Fatalf("%d stories: %q shown %d times", n, a.Title, c)
			}
		}
	}
	html := render(t, "index.html", gin.H{"Query": "আগুন", "Articles": testArticles("general", 4), "Page": 2, "Total": int64(24)})
	if !strings.Contains(html, "খোঁজার ফলাফল") || !strings.Contains(html, "২৪টি খবর") {
		t.Error("search results page lacks its heading")
	}
}

func TestArticleSavedAndErrorPagesRender(t *testing.T) {
	a := testArticles("bangladesh", 1)[0]
	head, tail := splitBody(a.Content+"<p>তৃতীয়</p>", 2)
	more := testArticles("international", 1)[0]
	html := render(t, "article.html", gin.H{"Article": &a, "Category": a.Category, "Tags": []string{"আগুন"}, "BodyHead": head, "BodyTail": tail, "MoreRead": &more,
		"Related": testArticles("bangladesh", 4), "Latest": testArticles("sports", 5), "HeaderNews": testArticles("bangladesh", 3)})
	if strings.Index(html, "দ্বিতীয়") > strings.Index(html, "আরও পড়ুন") || strings.Index(html, "আরও পড়ুন") > strings.Index(html, "তৃতীয়") {
		t.Error("the আরও পড়ুন card belongs between the second and third paragraphs")
	}
	if strings.Count(html, "data-share ") != 2 {
		t.Error("SHARE belongs under the title and next to the tags")
	}
	for _, want := range []string{"পড়তে পারেন", "সম্পর্কিত খবর", "<h3>উপশিরোনাম</h3>", "cta-small", "অনলাইন ডেস্ক", "data-save"} {
		if !strings.Contains(html, want) {
			t.Errorf("article page lacks %q", want)
		}
	}
	render(t, "saved.html", gin.H{"Category": "__saved"})
	render(t, "error.html", gin.H{"message": "নেই"})
}

func TestDailyStarDates(t *testing.T) {
	ts := time.Date(2026, 11, 8, 17, 24, 0, 0, time.FixedZone("BST", 6*3600))
	if got := dsDateTime(ts); got != "৮ নভেম্বর ২০২৬, ০৫:২৪ অপরাহ্ন" {
		t.Errorf("dsDateTime = %q", got)
	}
	if got := part(testArticles("x", 3), 2, 10); len(got) != 1 {
		t.Errorf("part should cut to the list, got %d", len(got))
	}
	if part(nil, 0, 2) != nil {
		t.Error("part of nothing is nothing")
	}
}

func TestSplitBodyAfterTheSecondParagraph(t *testing.T) {
	head, tail := splitBody("<p>এক</p>\n<h3>শিরোনাম</h3><p>দুই</p><p>তিন</p>", 2)
	if head != "<p>এক</p>\n<h3>শিরোনাম</h3><p>দুই</p>" || tail != "<p>তিন</p>" {
		t.Errorf("got %q | %q", head, tail)
	}
	for _, short := range []string{"<p>এক</p><p>দুই</p>", "<p>এক</p>", "শুধু লেখা"} {
		if head, tail := splitBody(short, 2); head != short || tail != "" {
			t.Errorf("%q should not be cut, got %q | %q", short, head, tail)
		}
	}
}

func ranked(list []domain.Article) []domain.RankedArticle {
	out := make([]domain.RankedArticle, len(list))
	for i, a := range list {
		out[i] = domain.RankedArticle{Article: a}
	}
	return out
}

func TestStoriesShowTheirOutletReadsAndMostReadRank(t *testing.T) {
	list := testArticles("bangladesh", 20)
	for i := range list {
		list[i].SourceName, list[i].Views = "Prothom Alo", int64(i)
	}
	most := ranked(testArticles("sports", 3))
	most[0].SourceName, most[0].Views, most[0].PopularOutlet, most[0].PopularRank = "BBC News বাংলা", 1234, "BBC News বাংলা", 2
	cats := map[string][]domain.Article{"sports": list[:5]}
	html := render(t, "index.html", gin.H{"Front": true, "Cat": cats, "MostRead": most, "Articles": list, "Page": 1})
	for _, want := range []string{"সর্বাধিক পঠিত: বিবিসি বাংলা · ২ নম্বর", "১২৩৪ বার পড়া হয়েছে", "সূত্র: প্রথম আলো", "১৬ বার পড়া হয়েছে", "এখনো পড়া হয়নি"} {
		if !strings.Contains(html, want) {
			t.Errorf("front page lacks %q", want)
		}
	}
	section := render(t, "index.html", gin.H{"Category": "bangladesh", "Articles": list, "MostRead": most, "Page": 1})
	if strings.Count(section, "সূত্র: প্রথম আলো") < len(list) {
		t.Error("every story on a section page shows its outlet")
	}
	a := list[3]
	page := render(t, "article.html", gin.H{"Article": &a, "BodyHead": a.Content, "PopularOutlet": "Prothom Alo", "PopularRank": 1})
	for _, want := range []string{"সূত্র: প্রথম আলো", "৩ বার পড়া হয়েছে", "সর্বাধিক পঠিত: প্রথম আলো · ১ নম্বর"} {
		if !strings.Contains(page, want) {
			t.Errorf("article page lacks %q", want)
		}
	}
}
