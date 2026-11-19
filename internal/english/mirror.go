package english

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// A local copy of the portal shows the hosted site's e-paper: the hosted
// site hands out each edition with its stories (/epaper/export), and a copy
// started with EN_MIRROR=<the hosted site's address> takes the last week's
// editions from it every 20 minutes instead of laying out its own. The
// stories are those the e-paper already shows to anyone.

// edExport is one edition as /epaper/export hands it out.
type edExport struct {
	Date     string          `json:"date"`
	Layout   int             `json:"layout"`
	Pages    json.RawMessage `json:"pages"`
	Articles []Article       `json:"articles"`
	Dates    []string        `json:"dates"` // the editions there are, newest first
}

// epaperExport hands out an edition (the latest, or the one of ?date=) with
// every story on its pages.
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
	out := edExport{Date: ed.Date, Layout: ed.Layout, Pages: json.RawMessage(ed.Pages), Articles: []Article{}}
	if len(ids) > 0 {
		s.db.Where("id IN ?", ids).Find(&out.Articles)
	}
	s.db.Model(&Edition{}).Order("date desc").Limit(14).Pluck("date", &out.Dates)
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
	go func() {
		time.Sleep(5 * time.Second)
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			if n, err := m.sync(ctx); err != nil {
				log.Printf("[en] e-paper mirror: %v", err)
			} else {
				log.Printf("[en] e-paper mirror: %d editions taken from %s", n, m.base)
			}
			cancel()
			time.Sleep(20 * time.Minute)
		}
	}()
	log.Printf("[en] the e-paper is copied from %s every 20 minutes", base)
}

// sync takes the latest edition and the six before it. A past day is taken
// again only when its pages changed; today's every time, as its stories get
// their full text during the day.
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
	for i, d := range latest.Dates {
		if i >= 7 || d == latest.Date {
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

// take stores an edition and its stories as the hosted site has them. A
// story this copy read from the same address under another id gives way.
func (m *mirror) take(ex edExport) error {
	if ex.Date == "" || len(ex.Pages) == 0 {
		return fmt.Errorf("an empty edition")
	}
	return m.db.Transaction(func(tx *gorm.DB) error {
		for _, a := range ex.Articles {
			if strings.HasPrefix(a.ImageURL, "/") { // a photo the hosted site keeps
				a.ImageURL = m.base + a.ImageURL
			}
			if err := tx.Where("source_url = ? AND id <> ?", a.SourceURL, a.ID).Delete(&Article{}).Error; err != nil {
				return err
			}
			if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&a).Error; err != nil {
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
}
