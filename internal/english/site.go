package english

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
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
	if err := db.AutoMigrate(&Article{}, &Seen{}, &Edition{}, &EpaperRead{}); err != nil {
		return fmt.Errorf("english: migrate: %w", err)
	}
	tpl, err := template.New("").Funcs(funcs()).ParseGlob(TemplateDir + "/*.html")
	if err != nil {
		return fmt.Errorf("english: templates: %w", err)
	}
	s := &site{db: db, tpl: tpl}
	s.routes(r)

	// A local copy can show the hosted site's e-paper (see mirror.go).
	mirrorFrom := strings.TrimSpace(os.Getenv("EN_MIRROR"))
	if mirrorFrom != "" {
		startMirror(db, mirrorFrom)
	}
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
			// About a day's English news plus the e-paper's stories; the AI
			// desk's free quotas (Gemini, then Groq, Cohere, Cloudflare) take it.
			rewrites = 150
		}
		w := &worker{db: db, rss: rss.NewClient(), images: images, ai: ai.NewGateway(cfg), rewrites: rewrites, failed: map[string]bool{}, tries: map[string]int{},
			mirrored: mirrorFrom != ""}
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
	r.GET("/epaper/faq", s.epaperFAQ)
	r.GET("/epaper/popular", s.epaperPopular)
	r.GET("/epaper/export", s.epaperExport)
	r.POST("/epaper/read/:id", s.epaperRead)
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
	c.Redirect(http.StatusFound, epaperURL(ed.Date, 1, 2))
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
		c.Redirect(http.StatusFound, epaperURL(ed.Date, 1, 2))
		return
	}
	pg, _ := strconv.Atoi(c.DefaultQuery("pgid", "1"))
	if pg < 1 || pg > len(pages) {
		pg = 1
	}
	view, _ := strconv.Atoi(c.DefaultQuery("view", "2"))
	if view != 1 {
		view = 2 // the page with the story clip beside it, as The Daily Star opens
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
	drawn, groups, main := drawPages(ed.Date, pages, byID)
	var dates []string
	s.db.Model(&Edition{}).Order("date desc").Limit(366).Pluck("date", &dates)
	day, _ := time.ParseInLocation("2006-01-02", ed.Date, dhaka)
	s.render(c, http.StatusOK, "en_epaper.html", map[string]any{
		"Date": ed.Date, "Day": day, "Pages": drawn, "Groups": groups, "Page": pg, "View": view, "Dates": dates,
		"PrevDate": neighbour(dates, ed.Date, 1), "NextDate": neighbour(dates, ed.Date, -1),
		"MinDate": last(dates), "MaxDate": first(dates), "AID": c.Query("aid"),
		"Ticker": s.latest(12), "BanglaDate": banglaDate(day), "PageCount": main,
	})
}

// drawPages sets an edition's pages for the template: each page's stories in
// the boxes of the Star's page it is set like, the stories that go on to
// another page tied to the box they go on in ("SEE PAGE 2 COL 4" and "FROM
// PAGE 1"), and the pages grouped by supplement.
func drawPages(date string, pages []EditionPage, byID map[string]Article) ([]drawnPage, []pageGroup, int) {
	var drawn []drawnPage
	var groups []pageGroup
	main := 0
	nth := map[string]int{}
	for _, p := range pages {
		d := drawnPage{No: p.No, Name: pageName(p.Name), Group: p.Group, Key: p.Layout}
		if d.Key == "" {
			n := p.Group + "/" + d.Name
			d.Key = keyForOld(date, p.Group, d.Name, nth[n])
			nth[n]++
		}
		for _, id := range p.Stories {
			if a, ok := byID[id]; ok {
				d.Stories = append(d.Stories, a)
			}
		}
		// Whole stories take the big places on the page; a story still
		// waiting for its full text goes among the briefs at the foot.
		sort.SliceStable(d.Stories, func(i, j int) bool { return printWeight(d.Stories[i]) > printWeight(d.Stories[j]) })
		lp, ok := printPageByKey(d.Key)
		if !ok {
			lp, d.Key = fallbackPage(len(d.Stories))
		}
		d.Title, d.W, d.H, d.Top = lp.Title, lp.W, lp.H, lp.Top
		d.Boxes = fillBoxes(lp, d.Stories)
		drawn = append(drawn, d)
	}
	// A story that goes on: its rest is set in the jump box the Star's plan
	// keeps for it on the later page.
	at := map[string]int{}
	for i, d := range drawn {
		at[d.Key] = i
	}
	for i := range drawn {
		for bi := range drawn[i].Boxes {
			b := &drawn[i].Boxes[bi]
			if len(b.to) != 2 || !b.Has || b.Kind != "story" || b.Story.Brief() {
				continue
			}
			k := drawn[i].Key[:strings.LastIndex(drawn[i].Key, "/")+1] + strconv.Itoa(b.to[0])
			j, ok := at[k]
			if !ok || b.to[1] >= len(drawn[j].Boxes) {
				continue
			}
			t := &drawn[j].Boxes[b.to[1]]
			if t.Kind != "jump" || t.Has {
				continue
			}
			b.JumpKey = fmt.Sprintf("j%d-%d", drawn[i].No, bi)
			t.JumpKey, t.Story, t.Has = b.JumpKey, b.Story, true
			b.JumpCol = gridCol(t.X)
			drawn[i].Boxes[bi].JumpPage = -(j + 1) // numbered below, once pages are grouped
			t.FromPage = -(i + 1)
		}
	}
	for i := range drawn {
		d := &drawn[i]
		if len(groups) == 0 || groups[len(groups)-1].Key != d.Group {
			label := d.Group
			if label == "" {
				label = "Dhaka Edition"
			}
			groups = append(groups, pageGroup{Key: d.Group, Label: label})
		}
		g := &groups[len(groups)-1]
		d.GNo = len(g.Pages) + 1 // a supplement numbers its pages from 1
		if d.Group == "" {
			main++
		}
		g.Pages = append(g.Pages, *d)
	}
	for i := range drawn {
		for bi := range drawn[i].Boxes {
			b := &drawn[i].Boxes[bi]
			if b.JumpPage < 0 {
				b.JumpPage = drawn[-b.JumpPage-1].GNo
			}
			if b.FromPage < 0 {
				b.FromPage = drawn[-b.FromPage-1].GNo
			}
		}
	}
	return drawn, groups, main
}

// fallbackPage is a plan for a page none fits: a news page of the Star's
// with room for its stories.
func fallbackPage(n int) (printPage, string) {
	var best printPage
	key := ""
	for _, d := range printDays {
		for ei, e := range d.Eds {
			if e.Name != "Dhaka Edition" {
				continue
			}
			for _, p := range e.Pages {
				if p.Title == "NEWS" && (key == "" || (p.capacity() >= n && (best.capacity() < n || p.capacity() < best.capacity()))) {
					best, key = p, layoutKey(d, ei, p.No)
				}
			}
		}
	}
	return best, key
}

// drawnPage is a printed page with its stories, for the template. GNo is its
// number within its supplement (the Dhaka Edition's pages and each
// supplement's are numbered from 1, as in print); No is its place in the
// whole edition, used in links.
type drawnPage struct {
	No      int
	GNo     int
	Name    string
	Group   string
	Stories []Article
	// The Star's page it is set like: its key, title ("NEWS"), size in the
	// paper's units, where the stories start under the folio, and the boxes.
	Key       string
	Title     string
	W, H, Top int
	Boxes     []drawnBox
}

// pageGroup is the Dhaka Edition or a supplement, with its pages.
type pageGroup struct {
	Key, Label string
	Pages      []drawnPage
}

// epaperRead counts one opening of an e-paper story (the reader's browser
// sends it when a story is opened), for "Most Popular Article".
func (s *site) epaperRead(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	date := c.Query("date")
	if _, derr := time.Parse("2006-01-02", date); err != nil || derr != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	var n int64
	s.db.Model(&Article{}).Where("id = ?", id).Count(&n)
	if n == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	s.db.Exec(`INSERT INTO en_epaper_reads (date, article_id, count) VALUES (?, ?, 1)
		ON CONFLICT (date, article_id) DO UPDATE SET count = en_epaper_reads.count + 1`, date, id.String())
	c.Status(http.StatusNoContent)
}

// epaperPopular lists a day's most opened e-paper stories (then the order
// they were printed in, while few have been read).
func (s *site) epaperPopular(c *gin.Context) {
	ed, pages, err := editionFor(s.db, c.Query("date"))
	if err != nil {
		c.JSON(http.StatusOK, []any{})
		return
	}
	var order []string
	pageOf := map[string]drawnPage{}
	g, gno := "-", 0
	for _, p := range pages {
		if p.Group != g {
			g, gno = p.Group, 0
		}
		gno++
		for _, id := range p.Stories {
			order = append(order, id)
			pageOf[id] = drawnPage{No: p.No, GNo: gno, Name: pageName(p.Name), Group: p.Group}
		}
	}
	var reads []EpaperRead
	s.db.Where("date = ?", ed.Date).Find(&reads)
	count := map[string]int{}
	for _, r := range reads {
		count[r.ArticleID] = r.Count
	}
	sort.SliceStable(order, func(i, j int) bool { return count[order[i]] > count[order[j]] })
	if len(order) > 10 {
		order = order[:10]
	}
	var list []Article
	s.db.Where("id IN ?", order).Find(&list)
	byID := map[string]Article{}
	for _, a := range list {
		byID[a.ID.String()] = a
	}
	type item struct {
		ID, Title, Image, Page string
		No, Reads              int
	}
	out := []item{}
	for _, id := range order {
		a, ok := byID[id]
		if !ok {
			continue
		}
		p := pageOf[id]
		label := fmt.Sprintf("%d: %s", p.GNo, strings.ToUpper(p.Name))
		if p.Group != "" {
			label = p.Group + " " + label
		}
		out = append(out, item{ID: id, Title: a.Title, Image: a.ImageURL, Page: label, No: p.No, Reads: count[id]})
	}
	c.JSON(http.StatusOK, out)
}

// epaperFAQ answers readers' questions about the e-paper.
func (s *site) epaperFAQ(c *gin.Context) {
	s.render(c, http.StatusOK, "en_epaper_faq.html", map[string]any{})
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
