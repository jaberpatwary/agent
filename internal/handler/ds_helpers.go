package handler

import (
	"fmt"
	"strconv"
	"time"

	"bangla-news-aggregator/internal/domain"
)

// Date and time in the style of The Daily Star Bangla.

var (
	bnDays   = []string{"রবিবার", "সোমবার", "মঙ্গলবার", "বুধবার", "বৃহস্পতিবার", "শুক্রবার", "শনিবার"}
	bnMonths = []string{"", "জানুয়ারি", "ফেব্রুয়ারি", "মার্চ", "এপ্রিল", "মে", "জুন",
		"জুলাই", "আগস্ট", "সেপ্টেম্বর", "অক্টোবর", "নভেম্বর", "ডিসেম্বর"}
)

func inDhaka(t time.Time) time.Time {
	if loc, err := time.LoadLocation("Asia/Dhaka"); err == nil {
		return t.In(loc)
	}
	return t
}

// dsToday is the masthead date: "শুক্রবার, নভেম্বর ১৩, ২০২৬".
func dsToday() string {
	now := inDhaka(time.Now())
	return fmt.Sprintf("%s, %s %s, %s", bnDays[now.Weekday()], bnMonths[now.Month()],
		toBanglaDigits(strconv.Itoa(now.Day())), toBanglaDigits(strconv.Itoa(now.Year())))
}

// dsDateTime is a story's time: "৮ নভেম্বর ২০২৬, ০৫:২৪ অপরাহ্ন".
func dsDateTime(t time.Time) string {
	t = inDhaka(t)
	period := "পূর্বাহ্ন"
	if t.Hour() >= 12 {
		period = "অপরাহ্ন"
	}
	h := t.Hour() % 12
	if h == 0 {
		h = 12
	}
	return fmt.Sprintf("%s %s %s, %s %s", toBanglaDigits(strconv.Itoa(t.Day())), bnMonths[t.Month()],
		toBanglaDigits(strconv.Itoa(t.Year())), toBanglaDigits(fmt.Sprintf("%02d:%02d", h, t.Minute())), period)
}

// dsAgo is "৩ ঘণ্টা আগে" for today's stories and the full time for older ones.
func dsAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "এইমাত্র"
	case d < time.Hour:
		return toBanglaDigits(strconv.Itoa(int(d.Minutes()))) + " মিনিট আগে"
	case d < 24*time.Hour:
		return toBanglaDigits(strconv.Itoa(int(d.Hours()))) + " ঘণ্টা আগে"
	}
	return dsDateTime(t)
}

// part returns list[from:to], cut to what the list has, so a section with
// fewer stories than its layout holds still renders.
func part(list []domain.Article, from, to int) []domain.Article {
	if to > len(list) {
		to = len(list)
	}
	if from >= to {
		return nil
	}
	return list[from:to]
}

const siteName = "দ্য ডেইলি স্টার বাংলা"
