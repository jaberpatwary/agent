package english

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// The e-paper: each night after midnight (Dhaka) the day's edition is laid
// out from the stories of the day before, page by page, as a printed paper.

type pagePlan struct {
	name  string
	max   int
	match func(Article) bool
}

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
func anyStory(Article) bool     { return true }

// editionPlan is the order of the printed pages. Front Page takes the day's
// leading stories first; every other page takes what is left that fits it.
var editionPlan = []pagePlan{
	{"Front Page", 9, notOpinion},
	{"City", 9, func(a Article) bool {
		return in(a, "news", "crime-justice", "accidents-and-fires", "healthcare", "environment")
	}},
	{"Nation", 9, func(a Article) bool {
		return in(a, "news", "politics", "governance", "education", "technology") || in(a, "youth")
	}},
	{"Business", 9, func(a Article) bool { return in(a, "business") }},
	{"World", 9, func(a Article) bool { return in(a, "news", "world") }},
	{"Sports", 9, func(a Article) bool { return in(a, "sports") }},
	{"Editorial", 6, func(a Article) bool { return in(a, "opinion", "editorial", "views") }},
	{"Op-Ed", 6, func(a Article) bool { return in(a, "opinion") || in(a, "slow-reads") }},
	{"Arts & Entertainment", 8, func(a Article) bool { return in(a, "culture") }},
	{"Life & Living", 8, func(a Article) bool { return in(a, "lifestyle") }},
	{"Back Page", 9, anyStory},
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

// layoutEdition places stories (newest first) on the planned pages.
func layoutEdition(stories []Article) []EditionPage {
	used := map[string]bool{}
	var pages []EditionPage
	for i, plan := range editionPlan {
		cands := []Article{}
		for _, a := range stories {
			if !used[a.ID.String()] && plan.match(a) {
				cands = append(cands, a)
			}
		}
		if i == 0 {
			// Stable: equal scores keep newest first.
			for x := 1; x < len(cands); x++ {
				for y := x; y > 0 && leadScore(cands[y]) > leadScore(cands[y-1]); y-- {
					cands[y], cands[y-1] = cands[y-1], cands[y]
				}
			}
		}
		if len(cands) > plan.max {
			cands = cands[:plan.max]
		}
		// A page with only a story or two would print mostly blank: its stories
		// wait for a later page (Back Page takes any) instead.
		if len(cands) == 0 || (i > 0 && i < len(editionPlan)-1 && len(cands) < minPageStories) {
			continue
		}
		p := EditionPage{Name: plan.name}
		for _, a := range cands {
			used[a.ID.String()] = true
			p.Stories = append(p.Stories, a.ID.String())
		}
		pages = append(pages, p)
	}
	for i := range pages {
		pages[i].No = i + 1
	}
	return pages
}

// minPageStories: the fewest stories an inner page is printed with.
const minPageStories = 4

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
	var n int64
	var existing Edition
	if db.Where("date = ?", date).First(&existing).Error == nil {
		// An edition stands once made, except a thin first one a new portal
		// laid out before it had a day's news: that one is made again.
		if storiesIn(existing) >= fullEditionStories {
			return nil
		}
		n = 1
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, dhaka)
	var stories []Article
	db.Where("published_at >= ? AND published_at < ?", midnight.Add(-24*time.Hour), midnight).
		Order("published_at desc").Limit(200).Find(&stories)
	if len(stories) < minEditionStories {
		stories = nil
		db.Where("published_at < ?", now).Order("published_at desc").Limit(80).Find(&stories)
		if len(stories) < firstEditionStories {
			return errors.New("not enough stories for a first edition yet")
		}
	}
	if len(stories) < minEditionStories {
		return errors.New("not enough stories for an edition yet")
	}
	pages := layoutEdition(stories)
	raw, err := json.Marshal(pages)
	if err != nil {
		return err
	}
	if n > 0 {
		if len(stories) <= storiesIn(existing) {
			return nil
		}
		return db.Model(&Edition{}).Where("date = ?", date).Update("pages", string(raw)).Error
	}
	return db.Create(&Edition{Date: date, Pages: string(raw)}).Error
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
