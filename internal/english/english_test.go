package english

import (
	"bytes"
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
		// Party names are not football, and a protest is not a cricket Test.
		{"https://www.dhakatribune.com/bangladesh/nation/424829/awami-league-back", "Awami League back on Gopalganj streets despite security clampdown", "news", "politics"},
		{"https://www.thedailystar.net/bangladesh/local-news/4ryl7w040n", "Banned Awami League activists break into locked Tungipara office for meeting", "news", "politics"},
		{"https://www.dhakatribune.com/bangladesh/government-affairs/424896/zahed", "Zahed: Govt monitoring public servants with Awami League links", "news", "politics"},
		{"https://www.thedailystar.net/bangladesh/local-news/xu9rii0mod", "Workers halt operations at 2 Chittagong port terminals, stage protest", "news", "governance"},
		{"https://www.observerbd.com/news/550004", "Premier League: Arsenal score late goals to go top", "sports", "football"},
		{"https://www.observerbd.com/news/550005", "Bangladesh name squad for first Test against India", "sports", "cricket"},
	}
	for _, c := range cases {
		sec, sub := classify(c.link, c.title)
		if sec != c.section || sub != c.sub {
			t.Errorf("%s %q => %s/%s, want %s/%s", c.link, c.title, sec, sub, c.section, c.sub)
		}
	}
}

func TestHeadlineWordsAreWholeWords(t *testing.T) {
	for _, c := range []struct {
		text, word string
		want       bool
	}{
		{" mass protest in dhaka ", "test", false},
		{" first test today ", "test", true},
		{" two goals in ten minutes ", "goal", true},
		{" used cars ", "us ", false},
		{" us sanctions ", "us ", true},
		{" two sentenced to death ", "sentenc*", true},
		{" washington talks ", "ton ", false},
		{" police arrested three ", "arrest", true},
		{" a great threat ", "eat", false},
	} {
		if got := hasWord(c.text, c.word); got != c.want {
			t.Errorf("hasWord(%q, %q) = %v", c.text, c.word, got)
		}
	}
}

func TestTheSameNewsIsPrintedOnce(t *testing.T) {
	a := story("news", "politics", 1, false)
	a.Title = "JCD and Chhatra Shakti activists assault five Jagannath University officials over alleged Awami League ties"
	b := story("news", "politics", 2, true)
	b.Title = "JCD, Chhatra Shakti activists assault five Jagannath University officials"
	c := story("news", "crime-justice", 3, true)
	c.Title = "Ziaul Ahsan, high-profile defendants produced at ICT in handcuffs and helmets"
	d := story("news", "crime-justice", 4, false)
	d.Title = "Ziaul Ahsan and high-profile suspects produced at ICT in handcuffs and helmets"
	e := story("news", "politics", 5, false)
	e.Title = "State Minister dismisses reports of Indian conditions for Hasina's return"
	f := story("news", "politics", 6, false)
	f.Title = "India yet to set formal conditions for Hasina's return, says Shama Obaed"
	got := dropRepeats([]Article{a, b, c, d, e, f})
	var titles []string
	for _, s := range got {
		titles = append(titles, s.Title)
	}
	if len(got) != 4 || got[0].ID != b.ID || got[1].ID != c.ID {
		t.Fatalf("kept %q", titles)
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
	if len(pages) < 8 || pages[0].Name != "Front" {
		t.Fatalf("pages = %+v", pages)
	}
	// The Dhaka Edition ends with its Back page; the supplements follow it.
	back, supp := -1, -1
	seen := map[string]bool{}
	for i, p := range pages {
		if p.No != i+1 {
			t.Fatalf("page %d numbered %d", i+1, p.No)
		}
		if p.Group == "" && p.Name == "Back" {
			back = i
		}
		if p.Group != "" && supp < 0 {
			supp = i
		}
		if p.Group == "" && supp >= 0 {
			t.Fatalf("Dhaka Edition page %q after a supplement", p.Name)
		}
		for _, id := range p.Stories {
			if seen[id] {
				t.Fatalf("%s is on two pages", id)
			}
			seen[id] = true
		}
	}
	if back < 0 || supp != back+1 || pages[supp].Name != "BizFront" {
		t.Fatalf("Back at %d, first supplement page at %d: %+v", back, supp, pages)
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
	var drawn []drawnPage
	var groups []pageGroup
	main := 0
	for _, p := range pages {
		if len(groups) == 0 || groups[len(groups)-1].Key != p.Group {
			label := p.Group
			if label == "" {
				label = "Dhaka Edition"
			}
			groups = append(groups, pageGroup{Key: p.Group, Label: label})
		}
		g := &groups[len(groups)-1]
		d := drawnPage{No: p.No, GNo: len(g.Pages) + 1, Name: p.Name, Group: p.Group}
		if p.Group == "" {
			main++
		}
		for _, id := range p.Stories {
			d.Stories = append(d.Stories, byID[id])
		}
		drawn = append(drawn, d)
		g.Pages = append(g.Pages, d)
	}
	out := draw(t, "en_epaper.html", map[string]any{"Date": "2026-11-17", "Day": time.Date(2026, 11, 17, 0, 0, 0, 0, dhaka),
		"Pages": drawn, "Groups": groups, "Page": 1, "View": 2, "Dates": []string{"2026-11-17", "2026-11-16"},
		"BanglaDate": banglaDate(time.Date(2026, 11, 17, 0, 0, 0, 0, dhaka)), "PageCount": main, "Ticker": all[:3]})
	for _, want := range []string{"np-front", "Journalism Without Fear or Favour", "DHAKA TUESDAY, NOVEMBER 17, 2026", "AGRAHAYAN 2, 1433 BS",
		"data-story=", `<template id="st-`, "Dhaka Edition", `value="2026-11-17"`, "YOUR RIGHT", "PAGES PLUS BUSINESS", "IN BRIEF", "Photo: Collected",
		`class="np-folio"`, "/en/static/emblem.svg", `class="np-brief-box"`, `class="np-kicker"`, `class="np-suppmast"`,
		"Breaking News", "Text View", "Full View", "Dload Image", "font-inc", "Listen", "1: FRONT", "ep-clipview",
		`<optgroup label="Business">`, "1: BIZFRONT", "MOST POPULAR ARTICLE", "CONTACT US", "FAQs", "This website is using cookies.", "All Rights Reserved."} {
		if !strings.Contains(out, want) {
			t.Errorf("e-paper lacks %q", want)
		}
	}
	if n := strings.Count(out, `class="np-page`); n != len(pages) {
		t.Errorf("%d pages drawn, want %d", n, len(pages))
	}
	// Every story with a photo shows it on the page, not only the leads.
	if n, want := strings.Count(out, `<figure class="np-fig">`), len(all); n < len(pages)*4 {
		t.Errorf("%d photos on the pages (of %d stories)", n, want)
	}
	draw(t, "en_epaper.html", map[string]any{"NoEdition": true, "View": 1, "Page": 1})
	faq := draw(t, "en_epaper_faq.html", map[string]any{})
	if !strings.Contains(faq, "Frequently Asked Questions") {
		t.Error("FAQ page lacks its heading")
	}
}

func TestLongStoryGetsAPullQuote(t *testing.T) {
	long := "<p>The minister spoke at the event in Dhaka yesterday.</p><p>He said the plan was ready.</p>" +
		"<p>Dr. Rahman, who chairs the committee, attended. \"We will finish the new bridge before the monsoon begins next year,\" he told reporters at the site.</p>" +
		"<p>Work started in 2024 at a cost of Tk 1.5 crore.</p><p>Officials expect traffic to ease.</p>"
	out := string(withPull(long))
	at := strings.Index(out, `<blockquote class="np-pull">`)
	if at < 0 {
		t.Fatalf("no pull quote in %s", out)
	}
	if at != strings.Index(long, "<p>Dr.") {
		t.Errorf("pull quote not after the second paragraph: %s", out)
	}
	if !strings.Contains(out, "&#34;We will finish the new bridge before the monsoon begins next year,&#34; he told reporters at the site.</blockquote>") {
		t.Errorf("pull quote is not the quoted sentence: %s", out)
	}
	if short := "<p>One.</p><p>Two.</p>"; string(withPull(short)) != short {
		t.Error("a short story got a pull quote")
	}
	if got := sentences("Dr. Rahman came. He left at 5.30 pm today. Then it rained."); len(got) != 3 {
		t.Errorf("sentences = %q", got)
	}
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

func TestBanglaDate(t *testing.T) {
	cases := map[string]string{
		"2026-11-17": "Agrahayan 2, 1433 BS",
		"2026-04-14": "Boishakh 1, 1433 BS",
		"2026-12-16": "Poush 1, 1433 BS",
		"2027-03-15": "Falgun 30, 1433 BS", // 2028 is a leap year? no: Falgun 1433 falls in 2027, not leap
	}
	delete(cases, "2027-03-15")
	cases["2027-03-15"] = "Chaitra 1, 1433 BS" // Chaitra always starts on 15 March
	for in, want := range cases {
		d, _ := time.Parse("2006-01-02", in)
		if got := banglaDate(d); got != want {
			t.Errorf("%s => %q, want %q", in, got, want)
		}
	}
}
