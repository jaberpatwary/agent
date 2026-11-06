package agent

import "testing"

func TestCheckFactConsistencyIgnoresTimestamps(t *testing.T) {
	source := []string{"আপডেট: 2026-11-06 20:15", "দুর্ঘটনায় ১২ জন নিহত ও ৪৫ জন আহত হয়েছেন।"}
	got := CheckFactConsistency(source, "<p>দুর্ঘটনায় ১২ জনের মৃত্যু হয়েছে, আহত ৪৫ জন।</p>")
	if !got.Consistent || len(got.MissingNumbers) != 0 {
		t.Fatalf("timestamp digits should be ignored, got %+v", got)
	}
}

func TestCheckFactConsistencyFlagsDroppedFigures(t *testing.T) {
	source := []string{"দুর্ঘটনায় ১২ জন নিহত, ৪৫ জন আহত, ক্ষতি ৩০০ কোটি টাকা।"}
	got := CheckFactConsistency(source, "<p>একটি বড় দুর্ঘটনা ঘটেছে।</p>")
	if got.Consistent {
		t.Fatalf("dropping every figure should be flagged, got %+v", got)
	}
}
