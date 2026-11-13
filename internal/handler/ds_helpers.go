package handler

import (
	"fmt"
	"strconv"
	"strings"
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

// splitBody cuts a story after its nth paragraph, where The Daily Star puts
// a "আরও পড়ুন" card. A story with no more paragraphs after that is not cut.
func splitBody(body string, n int) (head, tail string) {
	at := 0
	for i := 0; i < n; i++ {
		j := strings.Index(body[at:], "</p>")
		if k := strings.Index(body[at:], "</P>"); k >= 0 && (j < 0 || k < j) {
			j = k
		}
		if j < 0 {
			return body, ""
		}
		at += j + len("</p>")
	}
	if !strings.Contains(strings.ToLower(body[at:]), "<p") {
		return body, ""
	}
	return body[:at], body[at:]
}

// outletNames are the source outlets' names as readers know them.
var outletNames = map[string]string{
	"prothom alo": "প্রথম আলো", "bbc news বাংলা": "বিবিসি বাংলা", "bbc bangla": "বিবিসি বাংলা",
	"risingbd": "রাইজিংবিডি", "bd24live": "বিডি২৪লাইভ", "techshohor": "টেকশহর",
	"jugantor": "যুগান্তর", "ittefaq": "ইত্তেফাক", "samakal": "সমকাল", "janakantha": "জনকণ্ঠ",
	"inqilab": "ইনকিলাব", "manab zamin": "মানবজমিন", "naya diganta": "নয়া দিগন্ত",
}

func outletLabel(name string) string {
	if v, ok := outletNames[strings.ToLower(strings.TrimSpace(name))]; ok {
		return v
	}
	return strings.TrimSpace(name)
}

// readCount is how often a story has been read on this portal.
func readCount(n int64) string {
	if n <= 0 {
		return "এখনো পড়া হয়নি"
	}
	return toBanglaDigits(strconv.FormatInt(n, 10)) + " বার পড়া হয়েছে"
}

// popularFresh is how long an outlet's most-read list counts after it was
// read; the desk reads the lists on every run.
const popularFresh = 12 * time.Hour
