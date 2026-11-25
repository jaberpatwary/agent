package english

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// The Star's page plans, day by day. Every night the e-paper takes the plans
// of that day's printed paper from the Star's own e-paper
// (epbeta.thedailystar.net, its public page data, no login): the editions,
// their pages and every box on each page (story, photo, quote, IN BRIEF
// column, a story's rest under "FROM PAGE 1"). Only the geometry is kept, no
// text of the paper. The plans are stored (en_print_plans), so an edition is
// set exactly as the Star's paper of the same date; a day whose paper is not
// out yet takes the latest paper of the same weekday until it is.

// PrintPlan is one day's page plans of the Star's paper.
type PrintPlan struct {
	Date      string `gorm:"primaryKey;size:10"` // 2006-01-02
	Plan      string `gorm:"type:text"`          // JSON printDay
	CreatedAt time.Time
}

func (PrintPlan) TableName() string { return "en_print_plans" }

var plans = struct {
	sync.RWMutex
	days map[string]printDay
}{days: map[string]printDay{}}

func init() {
	for _, d := range embeddedPrintDays() {
		plans.days[d.Date] = d
	}
}

// allPrintDays is every day's plans known, oldest first.
func allPrintDays() []printDay {
	plans.RLock()
	defer plans.RUnlock()
	out := make([]printDay, 0, len(plans.days))
	for _, d := range plans.days {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func addPrintDay(d printDay) {
	plans.Lock()
	plans.days[d.Date] = d
	plans.Unlock()
}

// hasPrintDay reports whether the Star's paper of a date (2006-01-02) is known.
func hasPrintDay(date string) bool {
	plans.RLock()
	defer plans.RUnlock()
	_, ok := plans.days[date]
	return ok
}

// loadPrintPlans reads the stored plans.
func loadPrintPlans(db *gorm.DB) {
	var rows []PrintPlan
	db.Find(&rows)
	for _, r := range rows {
		var d printDay
		if json.Unmarshal([]byte(r.Plan), &d) == nil && len(d.Eds) > 0 {
			addPrintDay(d)
		}
	}
}

// --- fetching -------------------------------------------------------------------

const epbetaURL = "https://epbeta.thedailystar.net/"

var errNotOut = errors.New("the paper of that day is not out yet")

// planFetcher asks the Star's e-paper for a day's plans, one request at a
// time, paced.
type planFetcher struct {
	client *http.Client
	base   string
	pause  time.Duration
	last   time.Time // the last try, so a paper not out yet is asked again later
	mu     sync.Mutex
}

func newPlanFetcher() *planFetcher {
	return &planFetcher{client: &http.Client{Timeout: 40 * time.Second}, base: epbetaURL, pause: 250 * time.Millisecond}
}

// ensurePlan fetches and stores the plans of the Star's paper of day t, if
// not known yet, at most every 20 minutes.
func (f *planFetcher) ensurePlan(ctx context.Context, db *gorm.DB, t time.Time) {
	date := t.Format("2006-01-02")
	if hasPrintDay(date) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if time.Since(f.last) < 20*time.Minute {
		return
	}
	f.last = time.Now()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	d, err := f.fetchDay(ctx, t)
	if err != nil {
		log.Printf("[en] the Star's page plans of %s: %v", date, err)
		return
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return
	}
	if err := db.Save(&PrintPlan{Date: date, Plan: string(raw)}).Error; err != nil {
		log.Printf("[en] saving the page plans of %s: %v", date, err)
	}
	addPrintDay(d)
	var s []string
	for _, e := range d.Eds {
		s = append(s, fmt.Sprintf("%s %d pages", e.Name, len(e.Pages)))
	}
	log.Printf("[en] the Star's page plans of %s: %s", date, strings.Join(s, ", "))
}

func (f *planFetcher) get(ctx context.Context, method, path string, body any, v any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(f.pause):
	}
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, f.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; DailyStarEpaperLayout/1.0)")
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	res, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d", path, res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

type epEdition struct {
	EditionID   int    `json:"EditionId"`
	DisplayName string `json:"EditionDisplayName"`
}

type epPage struct {
	PageID      int    `json:"PageId"`
	PageNo      string `json:"PageNo"`
	Title       string `json:"NewsProPageTitle"`
	Width       int    `json:"PageWidth"`
	Height      int    `json:"PageHeight"`
	EditionDate string `json:"EditionDate"`
}

type epRect struct {
	ID      int     `json:"ObjectId"`
	Type    int     `json:"ObjectType"`
	Left    float64 `json:"Rect_Left"`
	Top     float64 `json:"Rect_Top"`
	Right   float64 `json:"Rect_Right"`
	Bottom  float64 `json:"Rect_Bottom"`
	Related int     `json:"RelatedStoryID"`
}

type epStory struct {
	Linked  int    `json:"LinkedStoryId"`
	Kicker  string `json:"HeadKicker"`
	Content []struct {
		Headlines []string `json:"Headlines"`
		Body      string   `json:"Body"`
	} `json:"StoryContent"`
}

// fetchDay asks for the editions of a date, their pages, each page's boxes
// and, for each story box, what kind of story the paper set in it.
func (f *planFetcher) fetchDay(ctx context.Context, t time.Time) (printDay, error) {
	date := t.Format("02/01/2006")
	var eds []epEdition
	if err := f.get(ctx, http.MethodGet, "Home/GetAllSupplement?edid=1&EditionDate="+date, nil, &eds); err != nil {
		return printDay{}, err
	}
	m := mapDay{Date: date}
	det := map[string]detail{}
	for _, e := range eds {
		if e.EditionID == 0 {
			continue
		}
		var pages []epPage
		if err := f.get(ctx, http.MethodPost, "Home/GetAllpagespost", map[string]any{"editionid": e.EditionID, "editiondate": date}, &pages); err != nil {
			return printDay{}, err
		}
		me := mapEd{Name: e.DisplayName}
		_, wanted := edGroup(e.DisplayName)
		for _, p := range pages {
			if p.PageID == 0 || p.EditionDate != date {
				continue
			}
			var rects []epRect
			if err := f.get(ctx, http.MethodGet, fmt.Sprintf("Home/getingRectangleObject?pageid=%d", p.PageID), nil, &rects); err != nil {
				return printDay{}, err
			}
			mp := mapPage{No: p.PageNo, Title: p.Title, W: p.Width, H: p.Height}
			for _, r := range rects {
				mp.Rects = append(mp.Rects, rect{ID: r.ID, T: r.Type, X1: int(r.Left), Y1: int(r.Top), X2: int(r.Right), Y2: int(r.Bottom), Rel: r.Related})
				if r.Type != 2 || !wanted {
					continue
				}
				var s epStory
				if err := f.get(ctx, http.MethodGet, fmt.Sprintf("Home/getstorydetail?Storyid=%d", r.ID), nil, &s); err != nil {
					continue // the box keeps its plain shape
				}
				det[fmt.Sprint(r.ID)] = storyDetail(s)
			}
			me.Pages = append(me.Pages, mp)
		}
		if len(me.Pages) > 0 {
			m.Eds = append(m.Eds, me)
		}
	}
	if !complete(m) {
		return printDay{}, errNotOut
	}
	return makePrintDay(m, det), nil
}

// complete reports whether a day's paper is all up: its Dhaka Edition from
// the front page to the back page, every page with its boxes. Just after
// midnight the Star puts its pages up one by one.
func complete(m mapDay) bool {
	if len(m.Eds) == 0 || m.Eds[0].Name != "Dhaka Edition" {
		return false
	}
	pages := m.Eds[0].Pages
	if len(pages) < 8 {
		return false
	}
	front, back, bare := false, false, 0
	for _, p := range pages {
		t := strings.ToUpper(strings.TrimSpace(p.Title))
		front = front || t == "FRONT"
		back = back || t == "BACK"
		stories := 0
		for _, r := range p.Rects {
			if r.T == 2 {
				stories++
			}
		}
		if stories == 0 && t != "ADVT" && t != "SUPPLEMENT" {
			bare++
		}
	}
	return front && back && bare <= 1
}

// storyDetail is what the plan keeps of a story the paper set in a box: how
// many headlines, a kicker, a byline, how much text, whether it is a story's
// rest from another page, and where it goes on.
func storyDetail(s epStory) detail {
	d := detail{Linked: s.Linked, Kicker: strings.TrimSpace(s.Kicker)}
	var body strings.Builder
	for _, c := range s.Content {
		for _, h := range c.Headlines {
			if h = strings.TrimSpace(h); h != "" {
				d.Heads = append(d.Heads, h)
			}
		}
		body.WriteString(c.Body)
	}
	b := body.String()
	text := strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(b, " "))), " ")
	d.Byline = strings.Contains(b, "▌")
	d.Chars = len(text)
	d.From = strings.Contains(text, "FROM PAGE") || strings.Contains(text, "From Page")
	d.See = strings.Contains(text, "SEE PAGE")
	return d
}

// --- the plans --------------------------------------------------------------------

type rect struct {
	ID  int `json:"id"`
	T   int `json:"t"`
	X1  int `json:"x1"`
	Y1  int `json:"y1"`
	X2  int `json:"x2"`
	Y2  int `json:"y2"`
	Rel int `json:"rel"`
}

type mapPage struct {
	No    string `json:"no"`
	Title string `json:"title"`
	W     int    `json:"w"`
	H     int    `json:"h"`
	Rects []rect `json:"rects"`
}

type mapEd struct {
	Name  string    `json:"name"`
	Pages []mapPage `json:"pages"`
}

type mapDay struct {
	Date string  `json:"date"` // 02/01/2006
	Eds  []mapEd `json:"eds"`
}

type detail struct {
	Linked int      `json:"linked"`
	Heads  []string `json:"heads"`
	Kicker string   `json:"kicker"`
	Byline bool     `json:"byline"`
	Chars  int      `json:"chars"`
	From   bool     `json:"from"`
	See    bool     `json:"see"`
}

// mkBox is a box while a page's plan is made.
type mkBox struct {
	printBox
	id, linked, area int
}

// makePrintDay turns a day's page maps into its plans: each story box with its
// pictures and kind, a box set inside a story (a quote) or a thin strip
// under a photo told apart, the boxes stretched over what the paper gave to
// advertisements, and the jumps tied to the boxes they go on in.
func makePrintDay(m mapDay, det map[string]detail) printDay {
	t, _ := time.Parse("02/01/2006", m.Date)
	d := printDay{Date: t.Format("2006-01-02"), Weekday: int(t.Weekday())}
	type where struct{ ed, page, box int }
	at := map[int]where{}
	var all [][]*mkBox // per page, in day order
	for ei, me := range m.Eds {
		e := printEd{Name: me.Name}
		for _, mp := range me.Pages {
			p := printPage{Title: strings.ToUpper(strings.TrimSpace(mp.Title)), W: mp.W, H: mp.H}
			fmt.Sscan(mp.No, &p.No)
			var boxes []*mkBox
			for _, r := range mp.Rects {
				if r.T != 2 {
					continue
				}
				b := &mkBox{printBox: printBox{X: r.X1, Y: r.Y1, W: r.X2 - r.X1, H: r.Y2 - r.Y1}, id: r.ID}
				for _, q := range mp.Rects {
					if q.T == 4 && (q.Rel == r.ID || inside(q, r)) && q.Y2-q.Y1 > 30 {
						b.Pics = append(b.Pics, [4]int{q.X1, q.Y1, q.X2 - q.X1, q.Y2 - q.Y1})
					}
				}
				x := det[fmt.Sprint(r.ID)]
				b.Heads, b.Kicker, b.Byline, b.Chars, b.linked = len(x.Heads), x.Kicker != "", x.Byline, x.Chars, x.Linked
				b.Kind = boxKind(b, x)
				b.area = b.W * b.H
				boxes = append(boxes, b)
			}
			for _, b := range boxes {
				if b.H < 50 {
					b.Kind = "strip"
					continue
				}
				for _, o := range boxes {
					if o != b && o.Kind == "story" && o.area > b.area && boxOverlap(b, o) > 0.85*float64(b.area) && len(b.Pics) == 0 && b.Kind == "story" {
						b.Kind = "quote"
					}
				}
			}
			// A photo box the paper set other stories inside (a package under
			// one heading): its room goes to the stories in it.
			for _, o := range boxes {
				if o.Kind != "photo" {
					continue
				}
				in := 0.0
				for _, b := range boxes {
					if b != o && b.area < o.area && boxOverlap(b, o) > 0.85*float64(b.area) {
						in += float64(b.area)
					}
				}
				if in >= 0.4*float64(o.area) {
					o.Kind = "strip"
				}
			}
			p.Top = pageTop(p)
			fillPage(&p, boxes)
			for bi, b := range boxes {
				at[b.id] = where{ei, len(e.Pages), bi}
			}
			for _, b := range boxes {
				p.Boxes = append(p.Boxes, b.printBox)
			}
			all = append(all, boxes)
			e.Pages = append(e.Pages, p)
		}
		d.Eds = append(d.Eds, e)
	}
	// Jumps: a story that goes on ("SEE PAGE 2 COL 4") points at the box on a
	// later page of its edition its rest is set in, and that box back at it.
	k := 0
	for ei := range d.Eds {
		for pi := range d.Eds[ei].Pages {
			p := &d.Eds[ei].Pages[pi]
			for bi, b := range all[k] {
				w, ok := at[b.linked]
				if !ok || b.linked == 0 || w.ed != ei || w.page == pi {
					continue
				}
				t := &d.Eds[ei].Pages[w.page]
				if w.box >= len(t.Boxes) || t.Boxes[w.box].Kind != "jump" || t.Boxes[w.box].From != nil {
					continue
				}
				p.Boxes[bi].To = []int{t.No, w.box}
				t.Boxes[w.box].From = []int{p.No, bi}
			}
			k++
		}
	}
	return d
}

func inside(q, r rect) bool {
	return q.X1 >= r.X1-4 && q.X2 <= r.X2+4 && q.Y1 >= r.Y1-4 && q.Y2 <= r.Y2+4
}

func boxKind(b *mkBox, x detail) string {
	for _, h := range x.Heads {
		if strings.EqualFold(strings.TrimSpace(h), "IN BRIEF") {
			return "brief"
		}
	}
	pics := 0
	for _, p := range b.Pics {
		pics += p[2] * p[3]
	}
	switch {
	case x.From:
		return "jump"
	case len(x.Heads) == 0 && len(b.Pics) > 0 && x.Chars < 700, len(b.Pics) > 0 && pics*100 >= b.W*b.H*55 && x.Chars < 900:
		return "photo"
	case len(x.Heads) == 1 && !x.Byline && len(b.Pics) == 0 && x.Chars < 150 && len(x.Heads[0]) > 60:
		return "quote"
	}
	return "story"
}

func boxRank(k string) int {
	return map[string]int{"story": 0, "brief": 1, "photo": 2, "quote": 3, "jump": 4, "strip": 5}[k]
}

func boxOverlap(a, b *mkBox) float64 {
	w := math.Min(float64(a.X+a.W), float64(b.X+b.W)) - math.Max(float64(a.X), float64(b.X))
	h := math.Min(float64(a.Y+a.H), float64(b.Y+b.H)) - math.Max(float64(a.Y), float64(b.Y))
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// pageTop is where a page's stories start: under the front page's masthead
// and its teasers, else under the folio or nameplate.
func pageTop(p printPage) int {
	if p.W < 900 { // a tabloid supplement
		return 60
	}
	switch p.Title {
	case "FRONT":
		return 236
	case "BIZFRONT":
		return 190
	case "BIZINNER", "BIZBACK", "BIZJACKET", "SUPPLEMENT":
		return 112
	case "SPORT":
		return 150
	}
	return 100
}

// fillPage stretches the boxes over what the paper gave to advertisements,
// so no part of the page is left empty: each box grows down to the box below
// it (or the foot of the page), up to the box above it (or the folio), and
// sideways to its neighbours (or the page's margins), wherever nothing else
// stands; then, again, down and up. A gutter is kept between boxes. Boxes
// end ordered stories first, biggest first.
func fillPage(p *printPage, boxes []*mkBox) {
	g := make([]*geom, len(boxes))
	for i, b := range boxes {
		g[i] = &geom{X: &b.X, Y: &b.Y, W: &b.W, H: &b.H, Inner: b.Kind == "quote" || b.Kind == "strip"}
	}
	stretch(p.W, p.H, p.Top, g)
	for _, b := range boxes {
		b.area = b.W * b.H
	}
	sort.SliceStable(boxes, func(i, j int) bool {
		a, b := boxes[i], boxes[j]
		if boxRank(a.Kind) != boxRank(b.Kind) {
			return boxRank(a.Kind) < boxRank(b.Kind)
		}
		return a.area > b.area
	})
}

// geom is a box's place, to be stretched; Inner is one set inside another
// box (a quote, a caption strip), which neither grows nor stops others.
type geom struct {
	X, Y, W, H *int
	Inner      bool
}

// stretch grows boxes over the empty parts of a page (W x H, its stories
// starting at top): down to the box below (or the foot), up to the box above
// (or the folio), sideways to the neighbours (or the margins), then down and
// up again, keeping a gutter between boxes. The order of boxes is kept.
func stretch(W, H, top int, boxes []*geom) {
	const gap = 22
	// A box set over a bigger one (a story's photo with its own caption, a
	// fact box) stays where it is: the bigger box's text runs round it.
	area := func(g *geom) int { return *g.W * *g.H }
	for _, a := range boxes {
		for _, b := range boxes {
			if a == b || a.Inner || b.Inner || area(a) >= area(b) {
				continue
			}
			w := min(*a.X+*a.W, *b.X+*b.W) - max(*a.X, *b.X)
			h := min(*a.Y+*a.H, *b.Y+*b.H) - max(*a.Y, *b.Y)
			if w > 0 && h > 0 && w*h*10 > area(a)*3 {
				a.Inner = true
			}
		}
	}
	left, right, bottom := 35, W-36, H-36
	if W < 900 {
		left, right, bottom = 24, W-24, H-24
	}
	overX := func(a, b *geom) bool { return *a.X < *b.X+*b.W-6 && *b.X < *a.X+*a.W-6 }
	overY := func(a, b *geom) bool { return *a.Y < *b.Y+*b.H-6 && *b.Y < *a.Y+*a.H-6 }
	order := func(key func(*geom) int) []*geom {
		s := append([]*geom(nil), boxes...)
		sort.SliceStable(s, func(i, j int) bool { return key(s[i]) < key(s[j]) })
		return s
	}
	down := func() {
		for _, b := range order(func(g *geom) int { return *g.Y }) {
			if b.Inner {
				continue
			}
			lim := bottom
			for _, o := range boxes {
				if o != b && !o.Inner && overX(o, b) && *o.Y >= *b.Y+*b.H-2 && *o.Y-gap < lim {
					lim = *o.Y - gap
				}
			}
			if lim > *b.Y+*b.H {
				*b.H = lim - *b.Y
			}
		}
	}
	up := func() {
		for _, b := range order(func(g *geom) int { return *g.Y }) {
			if b.Inner {
				continue
			}
			lim := top + 8
			for _, o := range boxes {
				if o != b && !o.Inner && overX(o, b) && *o.Y+*o.H <= *b.Y+2 && *o.Y+*o.H+gap > lim {
					lim = *o.Y + *o.H + gap
				}
			}
			if lim < *b.Y {
				*b.H += *b.Y - lim
				*b.Y = lim
			}
		}
	}
	sideways := func() {
		for _, b := range order(func(g *geom) int { return *g.X }) {
			if b.Inner {
				continue
			}
			lim := right
			for _, o := range boxes {
				if o != b && !o.Inner && overY(o, b) && *o.X >= *b.X+*b.W-2 && *o.X-gap < lim {
					lim = *o.X - gap
				}
			}
			if lim > *b.X+*b.W+gap { // more than a gutter of room
				*b.W = lim - *b.X
			}
			lim = left
			for _, o := range boxes {
				if o != b && !o.Inner && overY(o, b) && *o.X+*o.W <= *b.X+2 && *o.X+*o.W+gap > lim {
					lim = *o.X + *o.W + gap
				}
			}
			if lim < *b.X-gap {
				*b.W += *b.X - lim
				*b.X = lim
			}
		}
	}
	down()
	up()
	sideways()
	down()
	up()
}

// pageCover is the share of a page under its folio that its boxes cover.
func pageCover(p printPage) float64 {
	const s = 4
	n, in := 0, 0
	for y := p.Top + 10; y < p.H-40; y += s {
		for x := 40; x < p.W-40; x += s {
			n++
			for _, b := range p.Boxes {
				if x >= b.X-12 && x < b.X+b.W+12 && y >= b.Y-12 && y < b.Y+b.H+12 {
					in++
					break
				}
			}
		}
	}
	if n == 0 {
		return 0
	}
	return float64(in) / float64(n)
}
