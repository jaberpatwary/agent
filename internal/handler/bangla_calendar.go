package handler

import (
	"fmt"
	"time"
)

var banglaMonths = []string{"বৈশাখ", "জ্যৈষ্ঠ", "আষাঢ়", "শ্রাবণ", "ভাদ্র", "আশ্বিন", "কার্তিক", "অগ্রহায়ণ", "পৌষ", "মাঘ", "ফাল্গুন", "চৈত্র"}

// banglaCalendarDate converts a date to Bangladesh's revised (2019) Bangla
// calendar: 1 Boishakh is 14 April; Boishakh–Ashwin have 31 days, Falgun 29
// (30 when that February is in a leap year), every other month 30.
// Example: 16 Dec → "১ পৌষ", 14 Feb → "১ ফাল্গুন".
func banglaCalendarDate(t time.Time) string {
	if loc, err := time.LoadLocation("Asia/Dhaka"); err == nil {
		t = t.In(loc)
	}
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	startYear := t.Year()
	if day.Before(time.Date(startYear, time.April, 14, 0, 0, 0, 0, time.UTC)) {
		startYear--
	}
	start := time.Date(startYear, time.April, 14, 0, 0, 0, 0, time.UTC)
	offset := int(day.Sub(start).Hours() / 24)

	febYear := startYear + 1 // Falgun falls in February of the following Gregorian year
	falgun := 29
	if febYear%4 == 0 && (febYear%100 != 0 || febYear%400 == 0) {
		falgun = 30
	}
	lengths := []int{31, 31, 31, 31, 31, 31, 30, 30, 30, 30, falgun, 30}
	month := 0
	for month < 11 && offset >= lengths[month] {
		offset -= lengths[month]
		month++
	}
	return toBanglaDigits(fmt.Sprintf("%d", offset+1)) + " " + banglaMonths[month] + " " +
		toBanglaDigits(fmt.Sprintf("%d", startYear-593)) + " বঙ্গাব্দ"
}
