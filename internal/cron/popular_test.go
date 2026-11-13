package cron

import (
	"testing"

	"bangla-news-aggregator/internal/domain"
)

func TestProthomAloMostReadKeepsTheListOrder(t *testing.T) {
	hits, err := parseProthomAloMostRead([]byte(`{"items":[
		{"type":"story","story":{"headline":"প্রথম","url":"https://www.prothomalo.com/bangladesh/a1","first-published-at":1794484800000}},
		{"type":"collection","story":{}},
		{"type":"story","story":{"headline":"দ্বিতীয়","url":"https://www.prothomalo.com/sports/b2"}}]}`))
	if err != nil || len(hits) != 2 || hits[0].Title != "প্রথম" || hits[1].URL != "https://www.prothomalo.com/sports/b2" || hits[0].Published.IsZero() {
		t.Fatalf("got %+v, %v", hits, err)
	}
}

func TestBBCMostReadTakesOnlyTheMostReadLinks(t *testing.T) {
	hits, err := parseBBCMostRead([]byte(`<html><body>
		<a href="https://www.bbc.com/bengali/articles/top1" data-static-reverb-click="x%22most-read-promo%22">এক নম্বর</a>
		<a href="https://www.bbc.com/bengali/articles/promo" data-static-reverb-click="x%22top-stories%22">অন্য খবর</a>
		<a href="/bengali/articles/top2" data-static-reverb-click="most-read">দুই নম্বর</a>
	</body></html>`))
	if err != nil || len(hits) != 2 || hits[0].Title != "এক নম্বর" || hits[1].URL != "https://www.bbc.com/bengali/articles/top2" {
		t.Fatalf("got %+v, %v", hits, err)
	}
}

func TestPopularKeyMatchesFeedLinksWithTrackingParameters(t *testing.T) {
	for _, u := range []string{
		"https://www.bbc.com/bengali/articles/c6y97e232nqno?at_medium=RSS&at_campaign=rss",
		"https://www.bbc.com/bengali/articles/c6y97e232nqno/",
		"https://www.bbc.com/bengali/articles/c6y97e232nqno#comments",
	} {
		if got := domain.PopularKey(u); got != "https://www.bbc.com/bengali/articles/c6y97e232nqno" {
			t.Errorf("PopularKey(%q) = %q", u, got)
		}
	}
	if feedHost("https://feeds.bbci.co.uk/bengali/rss.xml") != "feeds.bbci.co.uk" || feedHost("https://www.prothomalo.com/feed") != "prothomalo.com" {
		t.Error("feed hosts are matched without www")
	}
}

func TestJugantorMostReadKeepsReadCounts(t *testing.T) {
	hits, err := parseJugantorMostRead([]byte(`[
		{"id":2,"title":"দ্বিতীয়","link":"https:\/\/www.jugantor.com\/national\/2","total_hit":800,"created_at":"2026-11-14 01:00:00"},
		{"id":1,"title":"প্রথম","link":"https:\/\/www.jugantor.com\/campus\/1183602","total_hit":19713,"created_at":"2026-11-14 02:00:03"}]`))
	if err != nil || len(hits) != 2 || hits[0].Hits != 19713 || hits[0].URL != "https://www.jugantor.com/campus/1183602" || hits[1].Hits != 800 || hits[0].Published.IsZero() {
		t.Fatalf("got %+v, %v", hits, err)
	}
}

func TestEveryOutletWithAMostReadListIsWiredToItsFeed(t *testing.T) {
	for feed, want := range map[string]bool{
		"https://www.prothomalo.com/feed":                  true,
		"https://feeds.bbci.co.uk/bengali/rss.xml":         true,
		"https://www.jugantor.com/news_sitemap.xml":        true,
		"https://samakal.com/rss":                          false,
		"https://dailynayadiganta.com/sitemap/news/{date}": false,
	} {
		if _, ok := popularSources[feedHost(feed)]; ok != want {
			t.Errorf("%s: most-read reader wired = %v, want %v", feed, ok, want)
		}
	}
}
