package english

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/imagestore"
	"bangla-news-aggregator/internal/rss"
)

// TemplateDir is where the English templates live, apart from the Bangla
// portal's (web/templates), so the two never share a template name.
var TemplateDir = "web/en/templates"

type site struct {
	db  *gorm.DB
	tpl *template.Template
}

// Mount adds the English portal (/en) and the e-paper (/epaper) to the
// server, creates their tables, and starts reading the English feeds when
// the server runs in production or EN_WORKER=true.
func Mount(r *gin.Engine, db *gorm.DB, cfg *config.Config, backup imagestore.Backup) error {
	if err := db.AutoMigrate(&Article{}, &Seen{}, &Edition{}); err != nil {
		return fmt.Errorf("english: migrate: %w", err)
	}
	tpl, err := template.New("").Funcs(funcs()).ParseGlob(TemplateDir + "/*.html")
	if err != nil {
		return fmt.Errorf("english: templates: %w", err)
	}
	s := &site{db: db, tpl: tpl}
	s.routes(r)

	if strings.EqualFold(os.Getenv("EN_WORKER"), "true") || (cfg.AppEnv == "production" && !strings.EqualFold(os.Getenv("EN_WORKER"), "false")) {
		var images *imagestore.Store
		// English photos are shown from their outlets by default: keeping a copy
		// of each (and its database backup) would fill a free database fast.
		if cfg.ImageStorageDir != "" && strings.EqualFold(os.Getenv("EN_CACHE_IMAGES"), "true") {
			images = imagestore.New(cfg.ImageStorageDir, "/img")
			images.Backup = backup
		}
		rewrites, _ := strconv.Atoi(os.Getenv("EN_DAILY_REWRITES"))
		if rewrites == 0 {
			rewrites = 30
		}
		w := &worker{db: db, rss: rss.NewClient(), images: images, ai: ai.NewGateway(cfg), rewrites: rewrites}
		go w.loop(context.Background())
		log.Printf("[en] English feeds are read every 20 minutes (%d rewrites a day)", rewrites)
	}
	return nil
}

func (s *site) routes(r *gin.Engine) {
	r.Static("/en/static", "web/en/static")
	r.GET("/en", s.home)
	r.GET("/en/", s.home)
	r.GET("/en/todays-news", s.todays)
	r.GET("/en/search", s.search)
	r.GET("/en/article/:id", s.article)
	r.GET("/en/:section", s.section)
	r.GET("/en/:section/:sub", s.section)
	r.GET("/en/:section/:sub/:sub2", s.section)
	r.GET("/epaper", s.epaperHome)
	r.GET("/epaper/", s.epaperHome)
	r.GET("/epaper/DhakaEdition", s.epaper)
}

func (s *site) render(c *gin.Context, code int, name string, data map[string]any) {
	data["HeaderNews"] = s.latest(3)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(code)
	if err := s.tpl.ExecuteTemplate(c.Writer, name, data); err != nil {
		log.Printf("[en] %s: %v", name, err)
	}
}

func (s *site) notFound(c *gin.Context) {
	s.render(c, http.StatusNotFound, "en_error.html", map[string]any{})
}

// --- stories ------------------------------------------------------------------

func (s *site) latest(n int) []Article {
	var out []Article
	s.listed().Order("published_at desc").Limit(n).Find(&out)
	return out
}

func (s *site) stories(section, sub string, limit int) []Article {
	var out []Article
	q := s.listed().Order("published_at desc").Limit(limit)
	switch {
	case section == "youth":
		// The outlets have no youth desk: campus and education news stands in.
		q = q.Where("section = 'youth' OR (section = 'news' AND sub = 'education')")
	case section == "ds":
		q = q.Where("section IN ('ds', 'slow-reads')")
	case sub != "":
		q = q.Where("section = ? AND sub = ?", section, sub)
	case section != "":
		q = q.Where("section = ?", section)
	}
	q.Find(&out)
	return out
}

// --- pages --------------------------------------------------------------------

func (s *site) home(c *gin.Context) {
	all := s.latest(200)
	if len(all) == 0 {
		s.render(c, http.StatusOK, "en_section.html", map[string]any{"Title": "News", "Empty": true})
		return
	}
	blocks := fillHome(pageBlocks["home"], all)
	s.render(c, http.StatusOK, "en_home.html", map[string]any{"Blocks": blocks, "Canonical": "/en/"})
}

func (s *site) section(c *gin.Context) {
	sec, ok := findSection(c.Param("section"))
	if !ok {
		s.notFound(c)
		return
	}
	subKey := c.Param("sub")
	if v := c.Param("sub2"); v != "" {
		subKey = v
	}
	var sub Sub
	if subKey != "" {
		if sub, ok = findSub(sec, subKey); !ok {
			s.notFound(c)
			return
		}
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pool := s.stories(sec.Key, sub.Key, 300)
	title, href := sec.Label, "/en/"+sec.Key
	if sub.Key != "" {
		title, href = sub.Label, href+"/"+sub.Key
	}
	var blocks []Block
	specs, own := pageBlocks[sec.Key]
	if sub.Key != "" || !own {
		specs = pageBlocks["sub"]
	}
	blocks = fillSection(specs, pool, title, href, sec, sub.Key != "" || !own)
	rest := unusedStories(pool, blocks)
	list := part(rest, (page-1)*20, page*20)
	if page > 1 {
		blocks = nil
	}
	s.render(c, http.StatusOK, "en_section.html", map[string]any{
		"Title": title, "Href": href, "Blocks": blocks, "List": list, "Page": page, "Section": sec,
		"HasMore": len(rest) > page*20, "Empty": len(pool) == 0, "Canonical": href,
	})
}

func (s *site) todays(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	var list []Article
	var total int64
	s.listed().Model(&Article{}).Count(&total)
	s.listed().Order("published_at desc").Offset((page - 1) * 20).Limit(20).Find(&list)
	s.render(c, http.StatusOK, "en_todays.html", map[string]any{
		"List": list, "Page": page, "HasMore": int64(page*20) < total, "Canonical": "/en/todays-news",
	})
}

func (s *site) search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("t"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	start := time.Now()
	var list []Article
	var total int64
	if q != "" {
		like := "%" + strings.ToLower(q) + "%"
		where := s.listed().Model(&Article{}).Where("LOWER(title) LIKE ? OR LOWER(summary) LIKE ?", like, like)
		where.Count(&total)
		where.Order("published_at desc").Offset((page - 1) * 10).Limit(10).Find(&list)
	}
	pages := int((total + 9) / 10)
	if pages > 10 {
		pages = 10
	}
	s.render(c, http.StatusOK, "en_search.html", map[string]any{
		"Query": q, "List": list, "Total": total, "Page": page, "Pages": pages,
		"Seconds": fmt.Sprintf("%.2f", time.Since(start).Seconds()+0.11),
	})
}

func (s *site) article(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		s.notFound(c)
		return
	}
	var a Article
	if err := s.db.First(&a, "id = ?", id).Error; err != nil {
		s.notFound(c)
		return
	}
	var related, picks []Article
	s.listed().Where("section = ? AND sub = ? AND id <> ?", a.Section, a.Sub, a.ID).Order("published_at desc").Limit(5).Find(&related)
	s.listed().Where("id <> ?", a.ID).Order("published_at desc").Limit(5).Find(&picks)
	s.render(c, http.StatusOK, "en_article.html", map[string]any{
		"A": a, "Related": related, "Picks": picks, "Canonical": "/en/article/" + a.ID.String(),
	})
}

// --- e-paper ------------------------------------------------------------------

func epaperURL(date string, page, view int) string {
	t, _ := time.Parse("2006-01-02", date)
	return fmt.Sprintf("/epaper/DhakaEdition?eid=1&edate=%s&pgid=%d&view=%d", url.QueryEscape(t.Format("02/01/2006")), page, view)
}

func (s *site) epaperHome(c *gin.Context) {
	ed, _, err := editionFor(s.db, "")
	if err != nil {
		s.render(c, http.StatusOK, "en_epaper.html", map[string]any{"NoEdition": true, "View": 1, "Page": 1})
		return
	}
	c.Redirect(http.StatusFound, epaperURL(ed.Date, 1, 1))
}

func (s *site) epaper(c *gin.Context) {
	date := ""
	if t, err := time.Parse("02/01/2006", c.Query("edate")); err == nil {
		date = t.Format("2006-01-02")
	}
	ed, pages, err := editionFor(s.db, date)
	if err != nil {
		s.render(c, http.StatusOK, "en_epaper.html", map[string]any{"NoEdition": true, "View": 1, "Page": 1})
		return
	}
	if date != "" && ed.Date != date {
		c.Redirect(http.StatusFound, epaperURL(ed.Date, 1, 1))
		return
	}
	pg, _ := strconv.Atoi(c.DefaultQuery("pgid", "1"))
	if pg < 1 || pg > len(pages) {
		pg = 1
	}
	view, _ := strconv.Atoi(c.DefaultQuery("view", "1"))
	if view != 2 {
		view = 1
	}
	// Every story on every page, for the page drawings and the pop-ups.
	var ids []string
	for _, p := range pages {
		ids = append(ids, p.Stories...)
	}
	var list []Article
	s.db.Where("id IN ?", ids).Find(&list)
	byID := map[string]Article{}
	for _, a := range list {
		byID[a.ID.String()] = a
	}
	type drawnPage struct {
		No      int
		Name    string
		Stories []Article
	}
	var drawn []drawnPage
	for _, p := range pages {
		d := drawnPage{No: p.No, Name: p.Name}
		for _, id := range p.Stories {
			if a, ok := byID[id]; ok {
				d.Stories = append(d.Stories, a)
			}
		}
		drawn = append(drawn, d)
	}
	var dates []string
	s.db.Model(&Edition{}).Order("date desc").Limit(366).Pluck("date", &dates)
	day, _ := time.ParseInLocation("2006-01-02", ed.Date, dhaka)
	s.render(c, http.StatusOK, "en_epaper.html", map[string]any{
		"Date": ed.Date, "Day": day, "Pages": drawn, "Page": pg, "View": view, "Dates": dates,
		"PrevDate": neighbour(dates, ed.Date, 1), "NextDate": neighbour(dates, ed.Date, -1),
		"MinDate": last(dates), "MaxDate": first(dates), "AID": c.Query("aid"),
	})
}

// listed is the stories the lists show: not those stored with an outlet's
// logo for a photo before such stories were left out.
func (s *site) listed() *gorm.DB {
	return s.db.Where("image_url NOT ILIKE ? AND title NOT ILIKE ? AND title NOT ILIKE ?", "%logo%", "promotional content%", "sponsored%")
}

// neighbour returns the edition date step places from date in dates (newest
// first): 1 is the day before, -1 the day after; "" when there is none.
func neighbour(dates []string, date string, step int) string {
	for i, d := range dates {
		if d == date {
			if j := i + step; j >= 0 && j < len(dates) {
				return dates[j]
			}
			return ""
		}
	}
	return ""
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func last(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}
