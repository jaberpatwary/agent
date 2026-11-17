package english

import (
	"fmt"
	"html/template"
	"regexp"
	"strings"
	"time"
)

// BlockSpec is one block of a page (blocks_gen.go, made from The Daily
// Star's English pages by cmd/_gen_en).
type BlockSpec struct {
	Key    string
	Title  string
	Layout int
	Cards  int
}

// Block is a block filled with stories.
type Block struct {
	Key   string
	Title string
	Href  string
	S     []Article
}

type used map[string]bool

func (u used) take(pool []Article, n int, ok func(Article) bool) []Article {
	var out []Article
	for _, a := range pool {
		if len(out) >= n {
			break
		}
		id := a.ID.String()
		if !u[id] && ok(a) {
			u[id] = true
			out = append(out, a)
		}
	}
	return out
}

// fillHome fills the front page: the opening block with the newest news,
// Spotlight with the next photo stories, Opinion with opinion, Feature with
// lifestyle and culture, In Case You Missed It with the rest.
func fillHome(specs []BlockSpec, all []Article) []Block {
	u := used{}
	var out []Block
	for _, sp := range specs {
		var s []Article
		href := "/en/todays-news"
		switch strings.ToLower(sp.Title) {
		case "":
			s = u.take(all, sp.Cards, notOpinion)
		case "opinion":
			s, href = u.take(all, sp.Cards, func(a Article) bool { return a.Section == "opinion" }), "/en/opinion"
		case "feature":
			s, href = u.take(all, sp.Cards, func(a Article) bool { return a.Section == "lifestyle" || a.Section == "culture" }), "/en/culture"
		default:
			s = u.take(all, sp.Cards, notOpinion)
		}
		if len(s) == 0 {
			continue
		}
		out = append(out, Block{Key: sp.Key, Title: sp.Title, Href: href, S: s})
	}
	return out
}

// fillSection fills a section page: a block titled with the section (or
// untitled) takes its newest stories, each sub-section block takes its own
// sub-section's, and a block nothing fits is left out.
func fillSection(specs []BlockSpec, pool []Article, title, href string, sec Section, whole bool) []Block {
	u := used{}
	var out []Block
	for i, sp := range specs {
		t := strings.ToLower(sp.Title)
		var s []Article
		blockTitle, blockHref := sp.Title, href
		sub, isSub := blockSubs[t]
		switch {
		case i == 0 && (whole || t == "" || t == strings.ToLower(sec.Label) || !isSub):
			s = u.take(pool, sp.Cards, func(Article) bool { return true })
			if t != "" {
				blockTitle = title
			}
		case isSub:
			s = u.take(pool, sp.Cards, func(a Article) bool { return a.Sub == sub })
			blockHref = "/en/" + sec.Key + "/" + sub
		default:
			s = u.take(pool, sp.Cards, func(Article) bool { return true })
		}
		if len(s) == 0 {
			continue
		}
		out = append(out, Block{Key: sp.Key, Title: blockTitle, Href: blockHref, S: s})
	}
	return out
}

func unusedStories(pool []Article, blocks []Block) []Article {
	shown := map[string]bool{}
	for _, b := range blocks {
		for _, a := range b.S {
			shown[a.ID.String()] = true
		}
	}
	var out []Article
	for _, a := range pool {
		if !shown[a.ID.String()] {
			out = append(out, a)
		}
	}
	return out
}

func part(list []Article, from, to int) []Article {
	if to > len(list) {
		to = len(list)
	}
	if from >= to {
		return nil
	}
	return list[from:to]
}

// --- template helpers -----------------------------------------------------------

var tagStrip = regexp.MustCompile(`<[^>]*>`)

func funcs() template.FuncMap {
	return template.FuncMap{
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(kv); i += 2 {
				m[fmt.Sprint(kv[i])] = kv[i+1]
			}
			return m
		},
		"at": func(list []Article, i int) *Article {
			if i < 0 || i >= len(list) {
				return nil
			}
			return &list[i]
		},
		"part":   part,
		"fields": strings.Fields,
		"add":    func(a, b int) int { return a + b },
		"sub":    func(a, b int) int { return a - b },
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i + 1
			}
			return s
		},
		"safeHTML":     func(s string) template.HTML { return template.HTML(s) },
		"plain":        func(s string) string { return strings.Join(strings.Fields(tagStrip.ReplaceAllString(s, " ")), " ") },
		"enToday":      func() string { return time.Now().In(dhaka).Format("Monday, January 2, 2006") },
		"year":         func() int { return time.Now().In(dhaka).Year() },
		"enAgo":        enAgo,
		"enDate":       func(t time.Time) string { return t.In(dhaka).Format("2 January 2006") },
		"enDateTime":   func(t time.Time) string { return t.In(dhaka).Format("2 January 2006, 15:04") },
		"iso":          func(t time.Time) string { return t.In(dhaka).Format(time.RFC3339) },
		"sectionLabel": sectionLabel,
		"subLabel":     subLabel,
		"upper":        strings.ToUpper,
		"printDate":    func(t time.Time) string { return strings.ToUpper(t.In(dhaka).Format("Monday, January 2, 2006")) },
		"epURL":        epaperURL,
		"dayParam":     func(d string) string { t, _ := time.Parse("2006-01-02", d); return t.Format("02/01/2006") },
		"dayLabel":     func(d string) string { t, _ := time.Parse("2006-01-02", d); return t.Format("2 Jan 2006") },
	}
}

// enAgo is The Daily Star's card time: "26 MIN(S)", "2 HOUR(s)", else the date.
func enAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "1 MIN(S)"
	case d < time.Hour:
		return fmt.Sprintf("%d MIN(S)", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d HOUR(s)", int(d.Hours()))
	}
	return t.In(dhaka).Format("2 January 2006")
}
