package cron

import "testing"

func TestTeasersAreRecognised(t *testing.T) {
	for _, s := range []string{
		"<p>ঢাকায় আজ ভারী বৃষ্টি হয়েছে। আবহাওয়া অফিস জানিয়েছে...</p>",
		"<p>ঢাকায় আজ ভারী বৃষ্টি হয়েছে […]</p>",
		"<p>ঢাকায় আজ ভারী বৃষ্টি হয়েছে। বিস্তারিত পড়ুন</p>",
	} {
		if !looksCutOff(s) {
			t.Errorf("teaser not recognised: %s", s)
		}
	}
	if looksCutOff("<p>ঢাকায় আজ ভারী বৃষ্টি হয়েছে। দিনভর এ অবস্থা থাকতে পারে বলে জানিয়েছে আবহাওয়া অফিস।</p>") {
		t.Error("a story that ends in a sentence is complete")
	}
}

func TestPageTextMustBeTheSameStory(t *testing.T) {
	feed := "<p>চট্টগ্রাম বন্দরে আজ সকাল থেকে কনটেইনার ওঠানামা বন্ধ রয়েছে বলে জানিয়েছে বন্দর কর্তৃপক্ষ।</p>"
	page := "<p>চট্টগ্রাম বন্দরে আজ সকাল থেকে কনটেইনার ওঠানামা বন্ধ রয়েছে বলে জানিয়েছে বন্দর কর্তৃপক্ষ।</p><p>শ্রমিকদের কর্মবিরতির কারণে এ অবস্থা।</p>"
	if !sameOpening(feed, page) {
		t.Fatal("the page that continues the feed text is the same story")
	}
	if sameOpening(feed, "<p>সর্বশেষ খবর: আজকের আবহাওয়া, খেলার খবর, বিনোদন।</p>") {
		t.Fatal("a page without the feed's opening is not the same story")
	}
}
