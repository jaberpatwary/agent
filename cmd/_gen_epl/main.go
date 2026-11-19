// Command _gen_epl makes internal/english/printlayouts.json: the page plans
// of The Daily Star's printed paper, taken from its e-paper
// (epbeta.thedailystar.net) for a run of days. For every page it keeps only
// the geometry: where each story, photo, quote, IN BRIEF column and jumped
// story ("FROM PAGE 1") sits, its pictures, how many headlines it carries and
// where a story continues ("SEE PAGE 2"). No text of the paper is kept.
//
//	go run ./cmd/_gen_epl <maps dir> <details dir>
//
// The maps are each day's editions, pages and boxes as the e-paper's own
// page asks for them (Home/GetAllSupplement, Home/GetAllpagespost,
// Home/getingRectangleObject); the details are each story box's
// Home/getstorydetail, reduced to counts and flags.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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

type mapDay struct {
	Date string `json:"date"`
	Eds  []struct {
		Name  string    `json:"name"`
		Pages []mapPage `json:"pages"`
	} `json:"eds"`
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

// Box is one block of a printed page, in the page's own units.
type Box struct {
	X, Y, W, H int
	Kind       string   `json:"k"`            // story, photo, quote, brief, jump
	Pics       [][4]int `json:"p,omitempty"`  // its pictures: x, y, w, h on the page
	Heads      int      `json:"hd,omitempty"` // headlines: 1 a head, 2 with a deck
	Kicker     bool     `json:"kk,omitempty"` // a kicker over the head
	Byline     bool     `json:"by,omitempty"`
	Chars      int      `json:"ch,omitempty"` // how much text the paper set in it
	To         []int    `json:"to,omitempty"` // continues on: page number, box index
	From       []int    `json:"fr,omitempty"` // a jump: from page number, box index
	id, linked int
	area       int
}

type Page struct {
	No    int    `json:"no"`
	Title string `json:"title"`
	W     int    `json:"w"`
	H     int    `json:"h"`
	Top   int    `json:"top"` // where the stories start, under the masthead or folio
	Boxes []*Box `json:"boxes"`
}

type Ed struct {
	Name  string  `json:"name"`
	Pages []*Page `json:"pages"`
}

type Day struct {
	Date    string `json:"date"`
	Weekday int    `json:"wd"`
	Eds     []*Ed  `json:"eds"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: _gen_epl <maps dir> <details dir>")
		os.Exit(2)
	}
	files, _ := filepath.Glob(filepath.Join(os.Args[1], "*.json"))
	sort.Strings(files)
	var days []*Day
	for _, f := range files {
		var m mapDay
		must(readJSON(f, &m))
		det := map[string]detail{}
		_ = readJSON(filepath.Join(os.Args[2], filepath.Base(f)), &det)
		days = append(days, makeDay(m, det))
	}
	out, err := json.Marshal(map[string]any{"days": days})
	must(err)
	must(os.WriteFile("internal/english/printlayouts.json", out, 0o644))
	for _, d := range days {
		for _, e := range d.Eds {
			var s []string
			for _, p := range e.Pages {
				s = append(s, fmt.Sprintf("%d:%s(%d,top %d,cover %.0f%%)", p.No, p.Title, len(p.Boxes), p.Top, cover(p)*100))
			}
			fmt.Printf("%s %s: %s\n", d.Date, e.Name, strings.Join(s, " "))
		}
	}
}

func makeDay(m mapDay, det map[string]detail) *Day {
	t, err := time.Parse("02/01/2006", m.Date)
	must(err)
	d := &Day{Date: t.Format("2006-01-02"), Weekday: int(t.Weekday())}
	where := map[int][2]int{} // story id -> page number, box index (filled after sorting)
	for _, me := range m.Eds {
		e := &Ed{Name: me.Name}
		for _, mp := range me.Pages {
			p := &Page{Title: strings.ToUpper(strings.TrimSpace(mp.Title)), W: mp.W, H: mp.H}
			fmt.Sscan(mp.No, &p.No)
			for _, r := range mp.Rects {
				if r.T != 2 {
					continue
				}
				b := &Box{X: r.X1, Y: r.Y1, W: r.X2 - r.X1, H: r.Y2 - r.Y1, id: r.ID}
				for _, q := range mp.Rects {
					if q.T == 4 && (q.Rel == r.ID || inside(q, r)) && q.Y2-q.Y1 > 30 {
						b.Pics = append(b.Pics, [4]int{q.X1, q.Y1, q.X2 - q.X1, q.Y2 - q.Y1})
					}
				}
				x := det[fmt.Sprint(r.ID)]
				b.Heads, b.Kicker, b.Byline, b.Chars, b.linked = len(x.Heads), x.Kicker != "", x.Byline, x.Chars, x.Linked
				b.Kind = kind(b, x)
				b.area = b.W * b.H
				p.Boxes = append(p.Boxes, b)
			}
			// A box set inside another story's (a pull quote, a fact box) is a
			// quote the story's text runs round; a thin strip under a photo is
			// its caption line.
			for _, b := range p.Boxes {
				if b.H < 50 {
					b.Kind = "strip"
					continue
				}
				for _, o := range p.Boxes {
					if o != b && o.Kind == "story" && o.area > b.area && overlap(b, o) > 0.85*float64(b.area) && len(b.Pics) == 0 && b.Kind == "story" {
						b.Kind = "quote"
					}
				}
			}
			sort.SliceStable(p.Boxes, func(i, j int) bool {
				a, b := p.Boxes[i], p.Boxes[j]
				if rank(a.Kind) != rank(b.Kind) {
					return rank(a.Kind) < rank(b.Kind)
				}
				return a.area > b.area
			})
			p.Top = top(p)
			fill(p)
			for i, b := range p.Boxes {
				where[b.id] = [2]int{p.No, i}
			}
			e.Pages = append(e.Pages, p)
		}
		d.Eds = append(d.Eds, e)
	}
	// Jumps: a story that goes on ("SEE PAGE 2 COL 4") points at the box on
	// a later page its rest is set in, and that box back at it (at the first
	// story, when the paper ties two to one box).
	for _, e := range d.Eds {
		for _, p := range e.Pages {
			for i, b := range p.Boxes {
				w, ok := where[b.linked]
				if !ok || b.linked == 0 || w[0] == p.No {
					continue
				}
				t := find(e, w[0])
				if t == nil || w[1] >= len(t.Boxes) || t.Boxes[w[1]].Kind != "jump" || t.Boxes[w[1]].From != nil {
					continue
				}
				b.To = []int{w[0], w[1]}
				t.Boxes[w[1]].From = []int{p.No, i}
			}
		}
	}
	return d
}

func find(e *Ed, no int) *Page {
	for _, p := range e.Pages {
		if p.No == no {
			return p
		}
	}
	return nil
}

func inside(q, r rect) bool {
	return q.X1 >= r.X1-4 && q.X2 <= r.X2+4 && q.Y1 >= r.Y1-4 && q.Y2 <= r.Y2+4
}

func kind(b *Box, x detail) string {
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

func rank(k string) int {
	return map[string]int{"story": 0, "brief": 1, "photo": 2, "quote": 3, "jump": 4, "strip": 5}[k]
}

// overlap is the area two boxes share.
func overlap(a, b *Box) float64 {
	w := math.Min(float64(a.X+a.W), float64(b.X+b.W)) - math.Max(float64(a.X), float64(b.X))
	h := math.Min(float64(a.Y+a.H), float64(b.Y+b.H)) - math.Max(float64(a.Y), float64(b.Y))
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// top is where a page's stories start: under the front page's masthead,
// else under the folio.
func top(p *Page) int {
	if p.W < 900 { // a tabloid supplement
		return 60
	}
	switch p.Title {
	case "FRONT":
		return 206
	case "BIZFRONT":
		return 190
	case "BIZINNER", "BIZBACK":
		return 112
	case "SPORT":
		return 150
	}
	return 100
}

// fill stretches the boxes over what the paper gave to advertisements: a box
// grows up to the box above it (or the masthead) and down to the box below
// it (or the foot of the page) where nothing else stands between.
func fill(p *Page) {
	bottom := p.H - 36
	if p.W < 900 {
		bottom = p.H - 24
	}
	const gap = 22
	// Quotes and caption strips sit inside other boxes: they neither grow
	// nor stop others growing.
	inner := func(b *Box) bool { return b.Kind == "quote" || b.Kind == "strip" }
	sort.SliceStable(p.Boxes, func(i, j int) bool { return p.Boxes[i].Y < p.Boxes[j].Y })
	for _, b := range p.Boxes {
		if inner(b) {
			continue
		}
		lim := bottom
		for _, o := range p.Boxes {
			if o != b && !inner(o) && overlapX(o, b) && o.Y >= b.Y+b.H-2 && o.Y-gap < lim {
				lim = o.Y - gap
			}
		}
		if lim > b.Y+b.H {
			b.H = lim - b.Y
		}
	}
	for _, b := range p.Boxes {
		if inner(b) {
			continue
		}
		lim := p.Top + 8
		for _, o := range p.Boxes {
			if o != b && !inner(o) && overlapX(o, b) && o.Y+o.H <= b.Y+2 && o.Y+o.H+gap > lim {
				lim = o.Y + o.H + gap
			}
		}
		if lim < b.Y {
			b.H += b.Y - lim
			b.Y = lim
		}
	}
	sort.SliceStable(p.Boxes, func(i, j int) bool {
		a, b := p.Boxes[i], p.Boxes[j]
		if rank(a.Kind) != rank(b.Kind) {
			return rank(a.Kind) < rank(b.Kind)
		}
		return a.area > b.area
	})
}

func overlapX(a, b *Box) bool {
	return a.X < b.X+b.W-6 && b.X < a.X+a.W-6
}

// cover is the share of the page under the masthead that boxes cover.
func cover(p *Page) float64 {
	const s = 4
	n, in := 0, 0
	for y := p.Top; y < p.H-36; y += s {
		for x := 30; x < p.W-30; x += s {
			n++
			for _, b := range p.Boxes {
				if x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H {
					in++
					break
				}
			}
		}
	}
	if n == 0 {
		return 0
	}
	return math.Round(float64(in)/float64(n)*100) / 100
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	raw = []byte(strings.TrimPrefix(string(raw), "\xef\xbb\xbf")) // a byte-order mark
	return json.Unmarshal(raw, v)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
