package english

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// The e-paper: each night after midnight (Dhaka) the day's edition is laid
// out from the stories of the day before, page by page, as a printed paper.

func in(a Article, section string, subs ...string) bool {
	if a.Section != section {
		return false
	}
	if len(subs) == 0 {
		return true
	}
	for _, s := range subs {
		if a.Sub == s {
			return true
		}
	}
	return false
}

func notOpinion(a Article) bool { return a.Section != "opinion" }

// homeNews is a news page's fare: anything from the news desk but the world.
func homeNews(a Article) bool {
	return (a.Section == "news" && a.Sub != "world") || a.Section == "youth"
}

// notSupplement: the Back page takes anything the supplements will not.
func notSupplement(a Article) bool { return a.Section != "business" && a.Section != "lifestyle" }

// printWeight orders a page's stories for print: whole stories first, then
// those with a summary, then a bare headline.
func printWeight(a Article) int {
	switch {
	case !a.Brief():
		return 2
	case strings.TrimSpace(a.Summary) != "":
		return 1
	}
	return 0
}

// pageName gives editions laid out before the pages were renamed the
// paper's names ("Front Page" is "Front", "World" is "International").
func pageName(name string) string {
	switch name {
	case "Front Page":
		return "Front"
	case "Nation":
		return "News"
	case "World":
		return "International"
	case "Sports":
		return "Sport"
	case "Op-Ed":
		return "Opinion"
	case "Arts & Entertainment":
		return "Art&Ent"
	case "Back Page":
		return "Back"
	}
	return name
}

// leadScore orders the front page: rewritten national news first.
func leadScore(a Article) int {
	s := 0
	if a.Rewritten {
		s += 4
	}
	if a.Section == "news" && a.Sub != "world" {
		s += 2
	}
	if a.Sub == "politics" || a.Sub == "governance" {
		s++
	}
	return s
}

// layoutEdition places stories (newest first) on the pages of the Star's
// paper of day (its Dhaka Edition, then its supplements), each piece of news
// once: when two outlets carry the same story, the fuller one is printed.
// Every page of the paper is printed while there is news for it:
//
//  1. each page takes its own kind of news, as many as it has places: the
//     front page first (the day's lead), then the supplements' pages (their
//     news is theirs alone), then the Dhaka Edition's pages in order;
//  2. a page with places left takes any news of its edition;
//  3. a page still without a story takes whatever of the day is left;
//  4. a page holding less text than the paper set on it takes more, as
//     spares set under a story too short for its box: of its own kind
//     first, then any news of its edition.
func layoutEdition(stories []Article, day time.Time) []EditionPage {
	stories = dropRepeats(stories)
	used := map[string]bool{}
	pd := printDayFor(day)
	type slot struct {
		page  EditionPage
		fill  pageFill
		front bool
	}
	var slots []*slot
	for ei, e := range pd.Eds {
		group, ok := edGroup(e.Name)
		if !ok {
			continue
		}
		for _, lp := range e.Pages {
			max := lp.capacity()
			if max == 0 { // a page of advertisements
				continue
			}
			plan := planForTitle(group, lp.Title)
			slots = append(slots, &slot{
				page:  EditionPage{Name: plan.name, Group: group, Layout: layoutKey(pd, ei, lp.No)},
				fill:  pageFill{plan: plan, group: group, max: max, short: lp.textNeed() * 9 / 10},
				front: group == "" && lp.Title == "FRONT",
			})
		}
	}
	add := func(s *slot, a Article) bool {
		if used[a.ID.String()] {
			return false
		}
		used[a.ID.String()] = true
		s.page.Stories = append(s.page.Stories, a.ID.String())
		s.fill.short -= textLen(a)
		return true
	}
	upTo := func(s *slot, n int, match func(Article) bool, lead bool) {
		if match == nil {
			return
		}
		for _, a := range pickStories(stories, used, match, lead) {
			if len(s.page.Stories) >= n {
				return
			}
			add(s, a)
		}
	}
	// 1.
	var order []*slot
	for _, pass := range []func(*slot) bool{
		func(s *slot) bool { return s.front },
		func(s *slot) bool { return s.fill.group != "" },
		func(s *slot) bool { return !s.front && s.fill.group == "" },
	} {
		for _, s := range slots {
			if pass(s) {
				order = append(order, s)
			}
		}
	}
	for _, s := range order {
		upTo(s, s.fill.max, s.fill.plan.match, s.front)
		upTo(s, s.fill.max, s.fill.plan.loose, false)
	}
	// 2.
	for _, s := range order {
		upTo(s, s.fill.max, s.fill.any(), false)
	}
	// 3. (the country's news first, then any)
	for _, s := range slots {
		if len(s.page.Stories) == 0 {
			upTo(s, s.fill.max, homeNews, false)
			upTo(s, s.fill.max, notOpinion, false)
		}
	}
	// 4.
	for _, s := range slots {
		for _, match := range []func(Article) bool{s.fill.plan.match, s.fill.plan.loose, s.fill.any()} {
			if match == nil {
				continue
			}
			for _, a := range pickStories(stories, used, match, false) {
				if s.fill.short <= 0 || len(s.page.Stories) >= s.fill.max+12 {
					break
				}
				add(s, a)
			}
		}
	}
	// Only a page nothing at all was left for is not printed.
	var out []EditionPage
	for _, s := range slots {
		if len(s.page.Stories) > 0 {
			s.page.No = len(out) + 1
			out = append(out, s.page)
		}
	}
	return out
}

// pageFill is how much text a laid-out page lacks of the paper's, and how
// many places it has.
type pageFill struct {
	plan  titlePlan
	group string
	max   int
	short int
}

// any is the news any page of an edition may take: the supplement's own, or
// for the Dhaka Edition anything but the supplements' and the opinion pages'.
func (f pageFill) any() func(Article) bool {
	switch f.group {
	case "Business":
		return func(a Article) bool { return in(a, "business") }
	case "Lifestyle":
		return func(a Article) bool { return in(a, "lifestyle") || in(a, "culture") }
	case "Campus", "Rising Star":
		return func(a Article) bool {
			return in(a, "youth") || in(a, "news", "education") || in(a, "news", "technology")
		}
	case "":
		return notSupplementOrOpinion
	}
	return nil
}

// pickStories is the stories not yet placed that match, the front page's
// main national news first, else whole stories before bare headlines;
// equal ones keep newest first.
func pickStories(stories []Article, used map[string]bool, match func(Article) bool, front bool) []Article {
	cands := []Article{}
	for _, a := range stories {
		if !used[a.ID.String()] && match(a) {
			cands = append(cands, a)
		}
	}
	score := printWeight
	if front {
		score = leadScore
	}
	sort.SliceStable(cands, func(i, j int) bool { return score(cands[i]) > score(cands[j]) })
	return cands
}

// dropRepeats keeps one story of each piece of news (by its headline's
// words), preferring one with its full text; the order is kept.
func dropRepeats(stories []Article) []Article {
	words := make([]map[string]bool, len(stories))
	for i, a := range stories {
		words[i] = headWords(a.Title)
	}
	drop := map[int]bool{}
	for i := range stories {
		for j := i + 1; j < len(stories); j++ {
			if drop[i] || drop[j] || !sameNews(words[i], words[j]) {
				continue
			}
			if stories[i].Brief() && !stories[j].Brief() {
				drop[i] = true
			} else {
				drop[j] = true
			}
		}
	}
	out := make([]Article, 0, len(stories))
	for i, a := range stories {
		if !drop[i] {
			out = append(out, a)
		}
	}
	return out
}

var notHeadWords = map[string]bool{"the": true, "and": true, "for": true, "with": true, "from": true, "over": true, "after": true, "into": true, "says": true, "said": true, "amid": true, "are": true, "was": true, "has": true, "have": true, "its": true, "his": true, "her": true, "their": true, "this": true, "that": true, "will": true, "not": true, "but": true}

func headWords(title string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if (len(w) >= 3 || w[0] >= '0' && w[0] <= '9') && !notHeadWords[w] {
			out[w] = true
		}
	}
	return out
}

// sameNews: two headlines share most of their words (at least four), or all.
func sameNews(a, b map[string]bool) bool {
	small, big := a, b
	if len(b) < len(a) {
		small, big = b, a
	}
	if len(small) == 0 {
		return false
	}
	common := 0
	for w := range small {
		if big[w] {
			common++
		}
	}
	if common == len(a) && common == len(b) {
		return true
	}
	return common >= 4 && (float64(common) >= 0.75*float64(len(small)) || float64(common) >= 0.6*float64(len(a)+len(b)-common))
}

// editionLayout is the version of the layout rules. An edition made with
// older rules is laid out again from the same day's stories. 7: the Star's
// page plans again (5, taken out as 6 on 2026-11-19), now of the very day's
// paper, the boxes stretched over its advertisements' space. 8: every page
// of the paper printed. 9: pages take their places before any page its
// spares, so a supplement's pages are not left without news.
const editionLayout = 9

// onOwnPlans reports whether an edition was set on the Star's paper of its
// own date.
func onOwnPlans(ed Edition) bool {
	var pages []EditionPage
	if json.Unmarshal([]byte(ed.Pages), &pages) != nil || len(pages) == 0 {
		return false
	}
	return strings.HasPrefix(pages[0].Layout, ed.Date+"/")
}

// minEditionStories: below this the night's run waits for more news.
const minEditionStories = 8

// firstEditionStories: a new portal with no stories from the day before waits
// for this many before laying out its first edition.
const firstEditionStories = 20

// ensureEdition makes today's edition (Dhaka date) if it is missing. It holds
// the stories published the day before; a new portal with less than a day
// of news takes the latest ones instead.
func ensureEdition(db *gorm.DB, now time.Time) error {
	date := now.Format("2006-01-02")
	var existing Edition
	found := db.Where("date = ?", date).First(&existing).Error == nil
	// An edition stands once made, except a thin first one a new portal laid
	// out before it had a day's news, or one made with older layout rules:
	// those are made again.
	// One set on another day's paper is made again once the plans of its own
	// day's paper come (and one set on plans since dropped, again).
	stale := found && (existing.Layout < editionLayout || hasPrintDay(date) != onOwnPlans(existing))
	if found && !stale && storiesIn(existing) >= fullEditionStories {
		return nil
	}
	stories, err := editionStories(db, now)
	if err != nil {
		return err
	}
	pages := layoutEdition(stories, now)
	raw, err := json.Marshal(pages)
	if err != nil {
		return err
	}
	if found {
		if !stale && len(stories) <= storiesIn(existing) {
			return nil
		}
		return db.Model(&Edition{}).Where("date = ?", date).
			Updates(map[string]any{"pages": string(raw), "layout": editionLayout}).Error
	}
	return db.Create(&Edition{Date: date, Pages: string(raw), Layout: editionLayout}).Error
}

// editionStories are the stories a day's edition is made from: those
// published the day before, or, for a new portal with less than a day of
// news, the latest ones.
func editionStories(db *gorm.DB, now time.Time) ([]Article, error) {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, dhaka)
	var stories []Article
	db.Where("published_at >= ? AND published_at < ?", midnight.Add(-24*time.Hour), midnight).
		Order("published_at desc").Limit(200).Find(&stories)
	if len(stories) >= minEditionStories {
		return stories, nil
	}
	stories = nil
	db.Where("published_at < ?", now).Order("published_at desc").Limit(80).Find(&stories)
	if len(stories) < firstEditionStories {
		return nil, errors.New("not enough stories for a first edition yet")
	}
	return stories, nil
}

// replaceInEdition swaps the stories of today's edition whose full text
// cannot be had (an outlet that does not let our server read its pages) for
// others of the same page's kind from the same day, whole stories first,
// never a second report of news already printed. It returns how many were
// swapped.
func replaceInEdition(db *gorm.DB, now time.Time, gone map[string]bool) (int, error) {
	date := now.Format("2006-01-02")
	var ed Edition
	if err := db.Where("date = ?", date).First(&ed).Error; err != nil {
		return 0, err
	}
	var pages []EditionPage
	if err := json.Unmarshal([]byte(ed.Pages), &pages); err != nil {
		return 0, err
	}
	stories, err := editionStories(db, now)
	if err != nil {
		return 0, err
	}
	var ids []string
	in := map[string]bool{}
	for _, p := range pages {
		for _, id := range p.Stories {
			in[id] = true
			ids = append(ids, id)
		}
	}
	var printed []Article
	db.Where("id IN ?", ids).Find(&printed)
	var heads []map[string]bool
	for _, a := range printed {
		if !gone[a.ID.String()] {
			heads = append(heads, headWords(a.Title))
		}
	}
	cands := dropRepeats(stories)
	sort.SliceStable(cands, func(i, j int) bool { return printWeight(cands[i]) > printWeight(cands[j]) })
	swapped := 0
	for pi := range pages {
		plan, planned := planFor(pages[pi].Group, pages[pi].Name)
		for si, id := range pages[pi].Stories {
			if !gone[id] {
				continue
			}
			for _, c := range cands {
				cid := c.ID.String()
				if in[cid] || gone[cid] || (planned && !plan.match(c)) || repeats(c, heads) {
					continue
				}
				pages[pi].Stories[si] = cid
				in[cid] = true
				heads = append(heads, headWords(c.Title))
				swapped++
				break
			}
		}
	}
	if swapped == 0 {
		return 0, nil
	}
	raw, err := json.Marshal(pages)
	if err != nil {
		return 0, err
	}
	return swapped, db.Model(&Edition{}).Where("date = ?", date).Update("pages", string(raw)).Error
}

// planFor finds a printed page's plan by its supplement and name.
func planFor(group, name string) (titlePlan, bool) {
	name = pageName(name)
	for _, t := range []string{"FRONT", "NEWS", "CITY", "INTERNATIONAL", "EDITORIAL", "OPINION", "ART&ENT", "SPORT", "BACK",
		"LAW", "HEALTH", "LITERATURE", "BUSINESS", "ROUNDTABLE", "BIZFRONT", "BIZINNER", "BIZBACK", "LS", "CAMPUS"} {
		if p := planForTitle(group, t); p.name == name {
			return p, true
		}
	}
	return titlePlan{}, false
}

func repeats(a Article, heads []map[string]bool) bool {
	w := headWords(a.Title)
	for _, h := range heads {
		if sameNews(w, h) {
			return true
		}
	}
	return false
}

// fullEditionStories: an edition with fewer stories than this was laid out
// from a new portal's first news and is made again as more arrives.
const fullEditionStories = 40

// storiesIn counts the stories of an edition.
func storiesIn(ed Edition) int {
	var pages []EditionPage
	if json.Unmarshal([]byte(ed.Pages), &pages) != nil {
		return 0
	}
	n := 0
	for _, p := range pages {
		n += len(p.Stories)
	}
	return n
}

// editionFor returns the edition of a day, or the latest one when that day
// has none.
func editionFor(db *gorm.DB, date string) (Edition, []EditionPage, error) {
	var ed Edition
	q := db.Order("date desc")
	if date != "" {
		q = q.Where("date <= ?", date)
	}
	if err := q.First(&ed).Error; err != nil {
		return ed, nil, err
	}
	var pages []EditionPage
	if err := json.Unmarshal([]byte(ed.Pages), &pages); err != nil {
		return ed, nil, fmt.Errorf("edition %s: %w", ed.Date, err)
	}
	return ed, pages, nil
}

// banglaDate is the Bangla calendar date printed on the paper's dateline
// ("Agrahayan 2, 1433 BS"), by Bangladesh's revised calendar: the year starts
// on 14 April; the first six months have 31 days, Falgun 29 (30 in a leap
// year) and the rest 30.
func banglaDate(t time.Time) string {
	months := []string{"Boishakh", "Joishtho", "Asharh", "Srabon", "Bhadro", "Ashwin", "Kartik", "Agrahayan", "Poush", "Magh", "Falgun", "Chaitra"}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	start := time.Date(t.Year(), 4, 14, 0, 0, 0, 0, time.UTC)
	if d.Before(start) {
		start = time.Date(t.Year()-1, 4, 14, 0, 0, 0, 0, time.UTC)
	}
	year := start.Year() - 593
	leap := func(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }
	falgun := 29
	if leap(start.Year() + 1) {
		falgun = 30
	}
	lengths := []int{31, 31, 31, 31, 31, 31, 30, 30, 30, 30, falgun, 30}
	day := int(d.Sub(start).Hours() / 24)
	for i, n := range lengths {
		if day < n {
			return fmt.Sprintf("%s %d, %d BS", months[i], day+1, year)
		}
		day -= n
	}
	return ""
}
