package handler

import (
	"testing"
	"time"
)

func TestBanglaCalendarDate(t *testing.T) {
	dhaka, _ := time.LoadLocation("Asia/Dhaka")
	cases := map[string]string{
		"2026-04-14": "১ বৈশাখ ১৪৩৩ বঙ্গাব্দ",
		"2026-04-13": "৩০ চৈত্র ১৪৩২ বঙ্গাব্দ",
		"2026-11-06": "২১ কার্তিক ১৪৩৩ বঙ্গাব্দ",
		"2026-12-16": "১ পৌষ ১৪৩৩ বঙ্গাব্দ",
		"2027-02-14": "১ ফাল্গুন ১৪৩৩ বঙ্গাব্দ",
		"2027-02-21": "৮ ফাল্গুন ১৪৩৩ বঙ্গাব্দ",
		"2026-03-26": "১২ চৈত্র ১৪৩২ বঙ্গাব্দ",
	}
	for in, want := range cases {
		d, _ := time.ParseInLocation("2006-01-02", in, dhaka)
		if got := banglaCalendarDate(d.Add(10 * time.Hour)); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}
