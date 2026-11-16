package english

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestClassifyReadsTheAddressThenTheHeadline(t *testing.T) {
	cases := []struct{ link, title, section, sub string }{
		{"https://www.thedailystar.net/news/bangladesh/politics/news/bnp-rally-4322481", "BNP plans rally", "news", "politics"},
		{"https://www.thedailystar.net/sports/cricket/news/tigers-win-1", "Tigers win", "sports", "cricket"},
		{"https://www.tbsnews.net/economy/stocks/dse-falls-123", "DSE falls", "business", "economy"},
		{"https://www.dhakatribune.com/world/asia/123/india-votes", "India votes", "news", "world"},
		{"https://en.prothomalo.com/opinion/editorial/abc", "Our view", "opinion", "editorial"},
		{"https://www.newagebd.net/post/film/322747/robert-pattinson-bond", "Pattinson is the new Bond", "culture", "tv-film"},
		{"https://www.observerbd.com/news/550000", "Fire guts 20 shops in Old Dhaka", "news", "accidents-and-fires"},
		{"https://www.observerbd.com/news/550001", "Court sentences two to death for murder", "news", "crime-justice"},
		{"https://www.observerbd.com/news/550002", "Litton's century keeps Tigers alive in ODI", "sports", "cricket"},
		{"https://www.observerbd.com/news/550003", "Adviser opens new bridge", "news", "governance"},
	}
	for _, c := range cases {
		sec, sub := classify(c.link, c.title)
		if sec != c.section || sub != c.sub {
			t.Errorf("%s %q => %s/%s, want %s/%s", c.link, c.title, sec, sub, c.section, c.sub)
		}
	}
}

func story(section, sub string, i int, rewritten bool) Article {
	a := Article{ID: uuid.New(), Title: fmt.Sprintf("%s %s story %d", section, sub, i), Summary: "A summary.",
		ImageURL: "/img/x.jpg", SourceName: "Dhaka Tribune", SourceURL: fmt.Sprintf("https://example.com/%s/%d", sub, i),
		Section: section, Sub: sub, Rewritten: rewritten, PublishedAt: time.Now().Add(-time.Duration(i) * time.Minute), CreatedAt: time.Now()}
	if rewritten {
		a.Content = "<p>First paragraph.</p><p>Second paragraph.</p>"
	}
	return a
}

func sampleStories() []Article {
	var out []Article
	i := 0
	for _, s := range sections {
		for _, sub := range s.Subs {
			for k := 0; k < 3; k++ {
				out = append(out, story(s.Key, sub.Key, i, k%2 == 0))
				i++
			}
		}
	}
	return out
}

func TestEditionPutsEachStoryOnOnePage(t *testing.T) {
	pages := layoutEdition(sampleStories())
	if len(pages) < 8 || pages[0].Name != "Front Page" || pages[len(pages)-1].Name != "Back Page" {
		t.Fatalf("pages = %+v", pages)
	}
	seen := map[string]bool{}
	for i, p := range pages {
		if p.No != i+1 {
			t.Fatalf("page %d numbered %d", i+1, p.No)
		}
		for _, id := range p.Stories {
			if seen[id] {
				t.Fatalf("%s is on two pages", id)
			}
			seen[id] = true
		}
	}
}

func tpl(t *testing.T) *template.Template {
	t.Helper()
	return template.Must(template.New("").Funcs(funcs()).ParseGlob("../../web/en/templates/*.html"))
}

func draw(t *testing.T, name string, data map[string]any) string {
	t.Helper()
	data["HeaderNews"] = sampleStories()[:3]
	var b bytes.Buffer
	if err := tpl(t).ExecuteTemplate(&b, name, data); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return b.String()
}

func TestEveryPageDraws(t *testing.T) {
	all := sampleStories()
	home := draw(t, "en_home.html", map[string]any{"Blocks": fillHome(pageBlocks["home"], all)})
	for _, want := range []string{"news-section-layout-40", "Top News", "/en/article/", "/en/static/swallow.css", "site-nav-header", "/epaper", "site-footer"} {
		if !strings.Contains(home, want) {
			t.Errorf("home lacks %q", want)
		}
	}
	for key, specs := range pageBlocks {
		sec, ok := findSection(key)
		if !ok {
			sec = sections[0]
		}
		blocks := fillSection(specs, all, sec.Label, "/en/"+sec.Key, sec, key == "sub")
		out := draw(t, "en_section.html", map[string]any{"Title": sec.Label, "Href": "/en/" + sec.Key, "Blocks": blocks, "List": all[:5], "Page": 1, "HasMore": true})
		for _, sp := range specs {
			if !strings.Contains(out, fmt.Sprintf("-layout-%d", sp.Layout)) {
				t.Errorf("%s page lacks layout %d", key, sp.Layout)
			}
		}
		if !strings.Contains(out, "data-next") {
			t.Errorf("%s page has no Load More", key)
		}
	}
	art := draw(t, "en_article.html", map[string]any{"A": all[0], "Related": all[1:4], "Picks": all[4:8]})
	for _, want := range []string{"Photo: Collected", "Source:", "Editor's Pick", "Related News", "Second paragraph"} {
		if !strings.Contains(art, want) {
			t.Errorf("article lacks %q", want)
		}
	}
	brief := draw(t, "en_article.html", map[string]any{"A": all[1]})
	if !strings.Contains(brief, "Read the full story at Dhaka Tribune") {
		t.Error("a brief does not link to its source")
	}
	draw(t, "en_todays.html", map[string]any{"List": all[:20], "Page": 1, "HasMore": true})
	draw(t, "en_search.html", map[string]any{"Query": "story", "List": all[:10], "Total": int64(40), "Page": 1, "Pages": 4, "Seconds": "0.21"})
	draw(t, "en_error.html", map[string]any{})
}

func TestEPaperDraws(t *testing.T) {
	all := sampleStories()
	pages := layoutEdition(all)
	byID := map[string]Article{}
	for _, a := range all {
		byID[a.ID.String()] = a
	}
	type drawnPage struct {
		No      int
		Name    string
		Stories []Article
	}
	var drawn []drawnPage
	for _, p := range pages {
		d := drawnPage{No: p.No, Name: p.Name}
		for _, id := range p.Stories {
			d.Stories = append(d.Stories, byID[id])
		}
		drawn = append(drawn, d)
	}
	raw, _ := json.Marshal(pages)
	_ = raw
	out := draw(t, "en_epaper.html", map[string]any{"Date": "2026-11-17", "Day": time.Date(2026, 11, 17, 0, 0, 0, 0, dhaka),
		"Pages": drawn, "Page": 1, "View": 2, "Dates": []string{"2026-11-17", "2026-11-16"}})
	for _, want := range []string{"np-front", "Journalism without fear or favour", "DHAKA TUESDAY, NOVEMBER 17, 2026", "data-story=", `<template id="st-`, "Dhaka Edition", `value="2026-11-17"`, "INSIDE", "IN BRIEF", "Text view", "data-goto", "Fit width", "Photo: Collected"} {
		if !strings.Contains(out, want) {
			t.Errorf("e-paper lacks %q", want)
		}
	}
	if n := strings.Count(out, `class="np-page`); n != len(pages) {
		t.Errorf("%d pages drawn, want %d", n, len(pages))
	}
	draw(t, "en_epaper.html", map[string]any{"NoEdition": true, "View": 1, "Page": 1})
}

func TestOutletLogosAreNotPhotos(t *testing.T) {
	for _, u := range []string{"https://images.thedailystar.net/sites/default/files/uploads/2023/06/18/ds-logo-share.jpeg", "https://x.com/default-image.png"} {
		if !placeholderImage(u) {
			t.Errorf("%s taken for a photo", u)
		}
	}
	if placeholderImage("https://www.thedailystar.net/sites/default/files/styles/big_201/public/images/2026/11/16/rally.jpg") {
		t.Error("a real photo was taken for a logo")
	}
}

func TestPaidContentIsNotNews(t *testing.T) {
	if !advertorial("Promotional Content: Shah Cement launches Water Shield") {
		t.Error("promotional content taken for news")
	}
	if advertorial("Cement exports rise 12%") {
		t.Error("a news headline taken for an advert")
	}
}

func TestThinPagesAreLeftOut(t *testing.T) {
	var all []Article
	for i := 0; i < 12; i++ {
		all = append(all, story("news", "politics", i, true))
	}
	all = append(all, story("sports", "cricket", 20, true), story("sports", "football", 21, true))
	pages := layoutEdition(all)
	for _, p := range pages {
		if p.Name == "Sports" {
			t.Fatalf("a Sports page with %d stories was laid out", len(p.Stories))
		}
	}
	total := 0
	for _, p := range pages {
		total += len(p.Stories)
	}
	if total != len(all) {
		t.Fatalf("%d of %d stories placed: the thin page's stories must go to a later page", total, len(all))
	}
}
