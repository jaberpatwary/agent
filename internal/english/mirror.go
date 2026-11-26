package english

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// A local copy of the portal shows the hosted site's e-paper: the hosted
// site hands out each edition with its stories and the Star's page plans it
// is set on (/epaper/export), and a copy started with EN_MIRROR=<the hosted
// site's address> takes the last week's editions from it every 20 minutes
// instead of laying out its own; its own templates, styles and scripts draw
// them. The stories are those the e-paper already shows to anyone.

// edExport is one edition as /epaper/export hands it out.
type edExport struct {
	Date     string          `json:"date"`
	Layout   int             `json:"layout"`
	Pages    json.RawMessage `json:"pages"`
	Articles []Article       `json:"articles"`
	Plans    []PrintPlan     `json:"plans"`  // the Star's page plans its pages are set on
	Dates    []string        `json:"dates"`  // the editions there are, newest first
	Ticker   []Article       `json:"ticker"` // the e-paper's breaking news, as the hosted site runs it
}

// mirrored is what a copy took from the hosted site besides the editions:
// its address (for "Most Popular Article") and its breaking news.
var mirrored struct {
	sync.RWMutex
	base   string
	ticker []string
}

func mirrorBase() string {
	mirrored.RLock()
	defer mirrored.RUnlock()
	return mirrored.base
}

// tickerOf is the breaking news the e-paper shows: the hosted site's in a
// copy, else this site's latest stories.
func (s *site) tickerOf() []Article {
	mirrored.RLock()
	ids := append([]string(nil), mirrored.ticker...)
	mirrored.RUnlock()
	if len(ids) == 0 {
		return s.latest(12)
	}
	var list []Article
	s.db.Where("id IN ?", ids).Find(&list)
	byID := map[string]Article{}
	for _, a := range list {
		byID[a.ID.String()] = a
	}
	out := make([]Article, 0, len(ids))
	for _, id := range ids {
		if a, ok := byID[id]; ok {
			out = append(out, a)
		}
	}
	return out
}

// popularFromMirror hands on the hosted site's "Most Popular Article", so a
// copy shows the same list; false when this is no copy or it cannot be had.
func popularFromMirror(c *gin.Context) bool {
	base := mirrorBase()
	if base == "" {
		return false
	}
	u := base + "/epaper/popular"
	if d := c.Query("date"); d != "" {
		u += "?date=" + url.QueryEscape(d)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil || res.StatusCode != http.StatusOK {
		return false
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
	return true
}

// epaperExport hands out an edition (the latest, or the one of ?date=) with
// every story on its pages and the page plans they are set on.
func (s *site) epaperExport(c *gin.Context) {
	ed, pages, err := editionFor(s.db, c.Query("date"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no edition"})
		return
	}
	var ids []string
	for _, p := range pages {
		ids = append(ids, p.Stories...)
	}
	days := planDays(pages)
	out := edExport{Date: ed.Date, Layout: ed.Layout, Pages: json.RawMessage(ed.Pages), Articles: []Article{}, Plans: []PrintPlan{}}
	if len(ids) > 0 {
		s.db.Where("id IN ?", ids).Find(&out.Articles)
	}
	if len(days) > 0 {
		s.db.Where("date IN ?", days).Find(&out.Plans)
	}
	s.db.Model(&Edition{}).Order("date desc").Limit(14).Pluck("date", &out.Dates)
	out.Ticker = s.tickerOf()
	c.Header("Cache-Control", "public, max-age=120")
	c.JSON(http.StatusOK, out)
}

// mirror copies the hosted site's editions into this copy's database.
type mirror struct {
	db     *gorm.DB
	base   string
	client *http.Client
	done   map[string]string // date -> pages already taken, for past days
}

func startMirror(db *gorm.DB, base string) {
	m := &mirror{db: db, base: strings.TrimRight(base, "/"), client: &http.Client{Timeout: 90 * time.Second}, done: map[string]string{}}
	mirrored.Lock()
	mirrored.base = m.base
	mirrored.Unlock()
	go func() {
		time.Sleep(5 * time.Second)
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			if n, err := m.sync(ctx); err != nil {
				log.Printf("[en] e-paper copy: %v", err)
			} else {
				log.Printf("[en] e-paper copy: %d editions taken from %s", n, m.base)
			}
			cancel()
			time.Sleep(20 * time.Minute)
		}
	}()
	log.Printf("[en] the e-paper is copied from %s every 20 minutes", base)
}

// sync takes the latest edition, every other the hosted site lists and its
// breaking news; an edition of this copy's own among those days goes. A past
// day is taken again only when its pages changed; today's every time, as its
// stories get their full text during the day.
func (m *mirror) sync(ctx context.Context) (int, error) {
	latest, err := m.fetch(ctx, "")
	if err != nil {
		return 0, err
	}
	n := 0
	if err := m.take(latest); err != nil {
		return n, err
	}
	n++
	if len(latest.Ticker) > 0 {
		ids := make([]string, 0, len(latest.Ticker))
		for _, a := range latest.Ticker {
			if err := m.store(m.db, a); err != nil {
				return n, err
			}
			ids = append(ids, a.ID.String())
		}
		mirrored.Lock()
		mirrored.ticker = ids
		mirrored.Unlock()
	}
	if len(latest.Dates) > 0 {
		oldest := latest.Dates[len(latest.Dates)-1]
		m.db.Where("date >= ? AND date NOT IN ?", oldest, latest.Dates).Delete(&Edition{})
	}
	for _, d := range latest.Dates {
		if d == latest.Date {
			continue
		}
		var have Edition
		if m.db.Where("date = ?", d).First(&have).Error == nil && m.done[d] != "" && m.done[d] == have.Pages {
			continue
		}
		ex, err := m.fetch(ctx, d)
		if err != nil {
			return n, err
		}
		if err := m.take(ex); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (m *mirror) fetch(ctx context.Context, date string) (edExport, error) {
	var ex edExport
	u := m.base + "/epaper/export"
	if date != "" {
		u += "?date=" + url.QueryEscape(date)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ex, err
	}
	res, err := m.client.Do(req)
	if err != nil {
		return ex, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ex, fmt.Errorf("%s: %s", u, res.Status)
	}
	return ex, json.NewDecoder(res.Body).Decode(&ex)
}

// take stores an edition, its stories and its page plans as the hosted site
// has them. A story this copy read from the same address under another id
// gives way.
func (m *mirror) take(ex edExport) error {
	if ex.Date == "" || len(ex.Pages) == 0 {
		return fmt.Errorf("an empty edition")
	}
	err := m.db.Transaction(func(tx *gorm.DB) error {
		for _, a := range ex.Articles {
			if err := m.store(tx, a); err != nil {
				return err
			}
		}
		for _, p := range ex.Plans {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "date"}}, DoUpdates: clause.AssignmentColumns([]string{"plan"})}).Create(&p).Error; err != nil {
				return err
			}
		}
		ed := Edition{Date: ex.Date, Pages: string(ex.Pages), Layout: ex.Layout, CreatedAt: time.Now()}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "date"}}, DoUpdates: clause.AssignmentColumns([]string{"pages", "layout"})}).Create(&ed).Error; err != nil {
			return err
		}
		m.done[ex.Date] = ed.Pages
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range ex.Plans {
		var d printDay
		if json.Unmarshal([]byte(p.Plan), &d) == nil && len(d.Eds) > 0 {
			addPrintDay(d)
		}
	}
	return nil
}

// store keeps a story as the hosted site has it.
func (m *mirror) store(tx *gorm.DB, a Article) error {
	if strings.HasPrefix(a.ImageURL, "/") { // a photo the hosted site keeps
		a.ImageURL = m.base + a.ImageURL
	}
	if err := tx.Where("source_url = ? AND id <> ?", a.SourceURL, a.ID).Delete(&Article{}).Error; err != nil {
		return err
	}
	return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&a).Error
}

// planDays are the dates of the Star's papers an edition's pages are set on.
func planDays(pages []EditionPage) []string {
	seen := map[string]bool{}
	for _, p := range pages {
		if d, _, ok := strings.Cut(p.Layout, "/"); ok {
			seen[d] = true
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
