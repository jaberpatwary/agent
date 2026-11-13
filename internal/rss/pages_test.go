package rss

import (
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func doc(t *testing.T, page string) *goquery.Document {
	t.Helper()
	d, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

const filler = "এই অনুচ্ছেদে খবরের বিস্তারিত তথ্য রয়েছে যা পাঠকের জন্য গুরুত্বপূর্ণ এবং যথেষ্ট লম্বা। "

// Known outlets: only their story box is read, not the related headlines
// or print-only stamps around it.
func TestKnownOutletBodyLeavesOutTheRestOfThePage(t *testing.T) {
	page := `<html><body><main>
<p class="title12">প্রিন্ট: ১৪ নভেম্বর ২০২৬, ০১:১৪ এএম</p>
<div class="detailBody"><p>প্রথম অনুচ্ছেদ: ` + strings.Repeat(filler, 3) + `</p><p>শেষ অনুচ্ছেদ: ` + strings.Repeat(filler, 2) + `</p></div>
<div class="more"><p>অন্য খবরের শিরোনাম যা এই খবরের অংশ নয় কিন্তু পাতায় আছে</p><p>আরেকটি অন্য খবরের শিরোনাম যা এখানে আসার কথা নয়</p></div>
</main></body></html>`
	got := extractArticleText(doc(t, page), "https://www.jugantor.com/capital/1183832")
	if !strings.Contains(got, "প্রথম অনুচ্ছেদ") || !strings.Contains(got, "শেষ অনুচ্ছেদ") || strings.Contains(got, "অন্য খবরের") || strings.Contains(got, "প্রিন্ট") {
		t.Fatalf("got %s", got)
	}
}

func TestOneParagraphWithLineBreaksBecomesParagraphs(t *testing.T) {
	page := `<html><body><div id="articleContent"><p>প্রথম লাইন: ` + strings.Repeat(filler, 2) + `<br><br>দ্বিতীয় লাইন: ` + strings.Repeat(filler, 2) + `<br/>তৃতীয় লাইন: ` + filler + `</p></div></body></html>`
	got := extractArticleText(doc(t, page), "https://www.mzamin.com/article/1/x")
	if n := strings.Count(got, "<p>"); n != 3 {
		t.Fatalf("want 3 paragraphs, got %d: %s", n, got)
	}
}

func TestDatelinesAreNotStoryText(t *testing.T) {
	for _, s := range []string{"প্রকাশ: ১৪ নভেম্বর ২০২৬ | ০০:৫০", "আপডেট: ১৪ নভেম্বর ২০২৬, ১২:১৯ এএম", "সম্পাদক, মুদ্রাকর ও প্রকাশক: কেউ একজন"} {
		if !isBoilerplate(s) {
			t.Errorf("%q should be left out", s)
		}
	}
	if isBoilerplate("প্রকাশ্যে মহড়া দিয়েছে কিশোর গ্যাং, এলাকায় আতঙ্ক ছড়িয়ে পড়েছে বলে জানিয়েছেন স্থানীয়রা") {
		t.Error("a story sentence is not a dateline")
	}
}

func TestCleanContent(t *testing.T) {
	if got := cleanContent("&lt;p&gt;খবর&lt;/p&gt;"); got != "<p>খবর</p>" {
		t.Errorf("escaped feed HTML: %q", got)
	}
	if got := cleanContent("<p>শেষ বাক্য। © Samakal</p>"); strings.Contains(got, "Samakal") || !strings.Contains(got, "</p>") {
		t.Errorf("copyright mark: %q", got)
	}
}

func TestPageTitleDropsTheOutletName(t *testing.T) {
	for page, want := range map[string]string{
		`<meta property="og:title" content="ইসলামী ব্যাংকে প্রস্তাব: Daily Naya Diganta (দৈনিক নয়া দিগন্ত)">`: "ইসলামী ব্যাংকে প্রস্তাব",
		`<meta property="og:title" content="হাসিনাকে ফেরত: ভারতের শর্ত | সমকাল">`:                              "হাসিনাকে ফেরত: ভারতের শর্ত",
		`<title>ডিসেম্বর সামনে রেখে নাশকতার ছক</title>`:                                                        "ডিসেম্বর সামনে রেখে নাশকতার ছক",
	} {
		if got := pageTitle(doc(t, "<html><head>"+page+"</head><body></body></html>")); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestSitemapKeepsArticlesNewestFirst(t *testing.T) {
	got := pickStories([]sitemapURL{
		{Loc: "https://www.mzamin.com/"},
		{Loc: " https://www.mzamin.com/archive "},
		{Loc: "https://www.mzamin.com/article/59495/তৃতীয়"},
		{Loc: "https://www.mzamin.com/article/59497/প্রথম"},
		{Loc: "https://www.mzamin.com/article/59497/প্রথম"},
		{Loc: "https://www.mzamin.com/article/59496/দ্বিতীয়"},
	})
	if len(got) != 3 || !strings.Contains(got[0].Loc, "59497") || !strings.Contains(got[2].Loc, "59495") {
		t.Fatalf("got %+v", got)
	}
	dated := pickStories([]sitemapURL{
		{Loc: "https://www.jugantor.com/capital/1", LastMod: "2026-11-13T10:00:00+06:00"},
		{Loc: "https://www.jugantor.com/capital/1183832", LastMod: "2026-11-13T09:00:00+06:00"},
		{Loc: "https://www.jugantor.com/national/1183833"},
	})
	if len(dated) != 2 || !strings.HasSuffix(dated[0].Loc, "/1183832") {
		t.Fatalf("articles need an id of 4+ digits, dated ones first: %+v", dated)
	}
	if !isSitemapFeed("https://dailynayadiganta.com/sitemap/news/{date}") || isSitemapFeed("https://samakal.com/rss") {
		t.Error("sitemap feeds are told apart by their address")
	}
}

func TestDhakaTimeLabeledUTCIsReadAsDhakaTime(t *testing.T) {
	now := time.Now()
	ahead := now.Add(5*time.Hour + 30*time.Minute) // a story from 30 minutes ago, labeled +00:00
	if got := clampPublished(ahead); got.After(now) || now.Sub(got) > 31*time.Minute || now.Sub(got) < 29*time.Minute {
		t.Errorf("got %v, want about 30 minutes ago", now.Sub(got))
	}
	if got := clampPublished(now.Add(48 * time.Hour)); got.After(time.Now()) {
		t.Error("a time far in the future is now")
	}
	if got := clampPublished(time.Time{}); time.Since(got) > time.Second {
		t.Error("an unknown time is now")
	}
}
