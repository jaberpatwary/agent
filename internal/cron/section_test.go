package cron

import "testing"

func TestStoriesGoToTheDailyStarSections(t *testing.T) {
	cases := []struct {
		link, title string
		labels      []string
		want        string
	}{
		{"https://www.prothomalo.com/opinion/column/abc", "সরকারের সামনে কঠিন পথ", nil, "opinion"},
		{"https://www.prothomalo.com/lifestyle/recipe/x", "পুরান ঢাকার নাশতা", nil, "lifestyle"},
		{"https://www.jugantor.com/campus/1183602", "ভর্তি পরীক্ষার ফল প্রকাশ", nil, "education"},
		{"https://samakal.com/agriculture/article/1/x", "আমনের ফলন ভালো", nil, "bangladesh"},
		{"https://samakal.com/whole-country/article/2/y", "সড়কে প্রাণ গেল দুজনের", nil, "bangladesh"},
		{"https://www.ittefaq.com.bd/820846/x", "এইচএসসির ফল", []string{"শিক্ষা"}, "education"},
		{"https://www.jugantor.com/sports/1", "সাকিবের সেঞ্চুরি", nil, "sports"},
	}
	for _, c := range cases {
		labels := append(append([]string(nil), c.labels...), sectionHints(c.link)...)
		if got := classifyItemCategory(c.title, labels, "bangladesh"); got != c.want {
			t.Errorf("%s: got %s, want %s", c.link, got, c.want)
		}
	}
}

func TestHeadlinesFillTheNewSections(t *testing.T) {
	for title, want := range map[string]string{
		"বইমেলায় নতুন উপন্যাস নিয়ে এলেন তরুণ লেখক":           "literature",
		"এইচএসসির ফল প্রকাশ, পাসের হার বেড়েছে":                "education",
		"ঘূর্ণিঝড়ের প্রভাবে উপকূলে টানা বৃষ্টি":               "environment",
		"হুমায়ুন কবির আমন্ত্রণ জানালেন হাতিয়ার সংসদ সদস্যকে": "bangladesh",
		"সাকিবের সেঞ্চুরিতে জয় পেল বাংলাদেশ":                  "sports",
	} {
		if got := classifyItemCategory(title, nil, "bangladesh"); got != want {
			t.Errorf("%q: got %s, want %s", title, got, want)
		}
	}
	if got := classifyItemCategory("x", sectionHints("https://www.prothomalo.com/opinion/analysis/abc"), "bangladesh"); got != "analysis" {
		t.Errorf("an analysis filed under opinion: got %s", got)
	}
}
