package agentic

import (
	"context"
	"strings"
	"testing"

	"bangla-news-aggregator/internal/cron"
)

func TestSameStory(t *testing.T) {
	same := [][2]string{
		{"টেকনাফে যৌথ বাহিনীর অভিযান, পাহাড়ে পড়ে আছে তিন লাশ", "পাহাড়ে চলছে যৌথ বাহিনীর অভিযান, জঙ্গলে পড়ে আছে ৩ লাশ"},
		{"বগুড়ায় পুলিশ সদস্যকে পিটিয়ে আসামি ছিনতাই", "বগুড়ায় পুলিশকে মারধর করে আসামি ছিনতাই"},
		// Same words with Bangla case endings (নভেম্বর/নভেম্বরের, প্রধানমন্ত্রী/প্রধানমন্ত্রীর).
		{"৭ নভেম্বর জাতির ঐক্যের দিন: প্রধানমন্ত্রী", "৭ নভেম্বরের চেতনায় জাতীয় ঐক্যের ডাক প্রধানমন্ত্রীর"},
		{"৭ নভেম্বর জাতির ঐক্যের দিন: প্রধানমন্ত্রী", "৭ নভেম্বরের অনুষ্ঠান: জাতীয় ঐক্যের তাগিদ প্রধানমন্ত্রীর"},
	}
	for _, p := range same {
		if !SameStory(p[0], p[1]) {
			t.Errorf("should match: %q / %q", p[0], p[1])
		}
	}
	different := [][2]string{
		{"টেকনাফে যৌথ বাহিনীর অভিযান, পাহাড়ে পড়ে আছে তিন লাশ", "ডেঙ্গুতে মারা গেলেন সাভারের তরুণ সাংবাদিক"},
		{"বাংলাদেশ ব্যাংক উদ্যোগ ঋণ নিয়ে সতর্ক করেছে", "বাংলাদেশ ক্রিকেট দল সিরিজ জিতেছে"},
		{"৭ নভেম্বর জাতির ঐক্যের দিন: প্রধানমন্ত্রী", "কানাডার ৮ আর্থিক প্রতিষ্ঠানের ওপর যুক্তরাষ্ট্রের নিষেধাজ্ঞা"},
		// প্রধান is not প্রধানমন্ত্রী: a long tail makes a different word.
		{"প্রধান শিক্ষকের বিরুদ্ধে মামলা", "প্রধানমন্ত্রীর বিরুদ্ধে মামলা প্রত্যাহার"},
	}
	for _, p := range different {
		if SameStory(p[0], p[1]) {
			t.Errorf("should not match: %q / %q", p[0], p[1])
		}
	}
}

func corroboratedStory(nr *fakeNewsroom) (*story, map[string]Tool) {
	s, tools, _ := newTestStory(nr, true)
	s.item.Title = "মিরপুরে পোশাক কারখানায় আগুন, আহত ৪৫"
	s.searchNews = func(context.Context, string, string) ([]NewsHit, error) {
		return []NewsHit{
			{Title: "মিরপুরে পোশাক কারখানায় ভয়াবহ আগুন", Source: "Outlet A"},
			{Title: "মিরপুরের কারখানায় আগুন, আহত ৪৫ শ্রমিক", Source: "Outlet B"},
			{Title: "শেয়ারবাজারে সূচকের পতন", Source: "Outlet C"},
		}, nil
	}
	return s, tools
}

func TestCorroboratedStoryCannotBeCalledFake(t *testing.T) {
	_, tools := corroboratedStory(&fakeNewsroom{})
	out := mustRun(t, tools, "search_news", `{"query":"মিরপুর আগুন"}`)
	if !strings.Contains(out, "carry this same story so far: 2") {
		t.Fatalf("two matching outlets should be counted (the unrelated one not): %s", out)
	}
	_, err := run(t, tools, "reject_story", `{"kind":"fake","explanation":"মনে হচ্ছে ভুল","evidence":["memory"]}`)
	if err == nil || !strings.Contains(err.Error(), "not fake") {
		t.Fatalf("a story carried by 2 other outlets must not be rejected as fake, got %v", err)
	}
}

func TestSubmitCarriesMeasuredCorroboration(t *testing.T) {
	nr := &fakeNewsroom{reviewerPasses: true}
	s, tools := corroboratedStory(nr)
	s.item.Content = origStory
	mustRun(t, tools, "search_news", `{"query":"মিরপুর আগুন"}`)
	// The model claims many outlets; only what code saw in search results counts.
	mustRun(t, tools, "record_verification", `{"is_real":true,"reason":"x","corroborating_outlets":["A","B","C","D","E"]}`)
	mustRun(t, tools, "review_draft", goodDraft)
	mustRun(t, tools, "submit_article", `{"decision":"publish","note":"ok"}`)
	if got := nr.saved[0].Corroborated; got != cron.MinCorroboration {
		t.Fatalf("corroboration must be the code-measured 2, got %d", got)
	}
}

func TestRememberFactNeedsOutletsFromSearch(t *testing.T) {
	var saved []string
	s, tools := corroboratedStory(&fakeNewsroom{})
	s.addFact = func(l string) error { saved = append(saved, l); return nil }
	if _, err := run(t, tools, "remember_fact", `{"fact":"X এখন মন্ত্রী","outlets":["Outlet A","Outlet B"]}`); err == nil {
		t.Fatal("a fact without any search must be refused")
	}
	mustRun(t, tools, "search_news", `{"query":"q"}`)
	if _, err := run(t, tools, "remember_fact", `{"fact":"X এখন মন্ত্রী","outlets":["Outlet A","Made Up"]}`); err == nil {
		t.Fatal("outlets that never appeared in search results must not count")
	}
	mustRun(t, tools, "remember_fact", `{"fact":"X এখন মন্ত্রী","outlets":["Outlet A","Outlet B"]}`)
	if len(saved) != 1 || !strings.Contains(saved[0], "Outlet A, Outlet B") {
		t.Fatalf("fact should be stored with its sources, got %v", saved)
	}
}
