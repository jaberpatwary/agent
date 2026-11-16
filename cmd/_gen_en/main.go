// One-off generator for the English portal: reads saved pages of
// www.thedailystar.net (full DOM, after scrolling) and writes
//
//	web/en/templates/chrome.html   header, menu bar, mega menu and footer
//	web/en/templates/blocks.html   every block of the home and section pages
//	internal/english/blocks_gen.go the blocks of each page, in order
//
//	go run ./cmd/_gen_en <dir with d_home.html, d_news.html, …>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type page struct{ file, key string }

var pages = []page{
	{"d_home.html", "home"},
	{"d_news.html", "news"},
	{"d_opinion.html", "opinion"},
	{"d_business.html", "business"},
	{"d_sports.html", "sports"},
	{"d_life.html", "lifestyle"},
	{"d_culture.html", "culture"},
	{"d_politics.html", "sub"}, // a sub-section page: one block, then the list
}

var (
	layoutRe = regexp.MustCompile(`\bnews-section-layout-(\d+)\b`)
	dateRe   = regexp.MustCompile(`^(Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday), [A-Z][a-z]+ \d{1,2}, \d{4}$`)
)

func attr(s *goquery.Selection, name string) string {
	v, _ := s.Attr(name)
	return strings.Join(strings.Fields(v), " ")
}

func tick(s string) string { return "`" + s + "`" }

// sections the portal has pages for; links to them stay inside /en.
var ownSections = []string{"news", "opinion", "business", "sports", "life-living", "lifestyle", "culture", "slow-reads", "youth", "ds"}

// mapHref points a Daily Star link at the portal's own page for it.
func mapHref(h string) string {
	h = strings.TrimSpace(h)
	switch {
	case strings.Contains(h, "bangla.thedailystar.net"):
		return "/"
	case strings.Contains(h, "epaper.thedailystar.net"):
		return "/epaper"
	case h == "/":
		return "/en/"
	case h == "/todays-news":
		return "/en/todays-news"
	case h == "/search":
		return "/en/search"
	case strings.HasPrefix(h, "/"):
		seg := strings.SplitN(strings.TrimPrefix(h, "/"), "/", 2)[0]
		for _, s := range ownSections {
			if seg == s {
				return "/en" + h
			}
		}
		return "https://www.thedailystar.net" + h
	}
	return h
}

// clean drops ads, scripts and sign-in links, and re-points links.
func clean(s *goquery.Selection) {
	s.Find(".block-dfp-block, script, noscript, iframe, style").Remove()
	s.Find("a[href*='registration'], a[href*='login'], a[href*='signin']").Each(func(_ int, a *goquery.Selection) {
		if li := a.Closest("li"); li.Length() > 0 {
			li.Remove()
		} else {
			a.Remove()
		}
	})
	s.Find("[data-drupal-link-system-path]").RemoveAttr("data-drupal-link-system-path")
	s.Find("[hreflang]").RemoveAttr("hreflang")
	s.Find("a[href]").Each(func(_ int, a *goquery.Selection) { a.SetAttr("href", mapHref(attr(a, "href"))) })
	s.Find("form[action]").Each(func(_ int, f *goquery.Selection) { f.SetAttr("action", mapHref(attr(f, "action"))) })
	s.Find("img[src*='logo']").SetAttr("src", "/en/static/logo.svg")
}

func outer(s *goquery.Selection) string {
	h, err := goquery.OuterHtml(s)
	if err != nil {
		panic(err)
	}
	return h
}

// --- blocks -----------------------------------------------------------------

type gen struct {
	n   int
	out strings.Builder
}

func (g *gen) card(c *goquery.Selection) {
	idx := g.n
	g.n++
	col := c.Find(".card-image-col").First()
	img := c.Find(".card-image").First()
	content := c.Find(".card-content").First()
	var order []string
	titleTag, titleCls, introCls := "h5", "card-title ", ""
	content.Children().Each(func(_ int, ch *goquery.Selection) {
		name := goquery.NodeName(ch)
		switch {
		case len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6':
			titleTag, titleCls = name, attr(ch, "class")+" "
			order = append(order, "title")
		case ch.HasClass("card-intro"):
			introCls = attr(ch, "class")
			order = append(order, "intro")
		case ch.HasClass("card-info"):
			order = append(order, "info")
		}
	})
	if introCls == "" {
		if in := c.Find(".card-intro").First(); in.Length() > 0 {
			introCls = attr(in, "class")
			order = append(order, "intro")
		}
	}
	if !strings.Contains(strings.Join(order, " "), "info") && c.Find(".card-info").Length() > 0 {
		order = append(order, "info")
	}
	if !strings.Contains(strings.Join(order, " "), "title") {
		order = append([]string{"title"}, order...)
	}
	fmt.Fprintf(&g.out, "{{ with at .S %d }}{{ template \"en_card\" (dict \"A\" . \"Card\" %s \"HasImg\" %v \"Col\" %s \"Img\" %s \"Content\" %s \"Tag\" %s \"TitleCls\" %s \"IntroCls\" %s \"Order\" %s) }}{{ end }}\n",
		idx, tick(attr(c, "class")), img.Length() > 0, tick(attr(col, "class")), tick(attr(img, "class")),
		tick(attr(content, "class")), tick(titleTag), tick(strings.TrimRight(titleCls, " ")+" "), tick(introCls), tick(strings.Join(order, " ")))
}

// containers are the elements a block nests its cards in.
var containers = map[string]bool{"div": true, "section": true, "aside": true, "article": true, "ul": true, "li": true}

func isHeading(name string) bool {
	return len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6'
}

// walk copies containers, replaces cards, and keeps headings and "more"
// links that sit outside cards (TOP NEWS, MORE NEWS).
func (g *gen) walk(s *goquery.Selection) {
	s.Children().Each(func(_ int, ch *goquery.Selection) {
		name := goquery.NodeName(ch)
		if ch.HasClass("section-title-wrapper") {
			g.out.WriteString("{{ if .Title }}<div class=\"section-title-wrapper\"><h2 class=\"section-title\"><a href=\"{{ .Href }}\"> {{ .Title }} </a></h2></div>{{ end }}\n")
			return
		}
		if isHeading(name) && ch.Find(".card").Length() == 0 {
			fmt.Fprintf(&g.out, "<%s class=%q>%s</%s>\n", name, attr(ch, "class"), strings.TrimSpace(ch.Text()), name)
			return
		}
		if name == "a" && strings.Contains(strings.ToLower(ch.Text()), "more") {
			fmt.Fprintf(&g.out, "<a class=%q href=\"{{ .Href }}\">%s</a>\n", attr(ch, "class"), strings.TrimSpace(ch.Text()))
			return
		}
		if !containers[name] {
			return
		}
		if name == "div" && ch.HasClass("card") {
			g.card(ch)
			return
		}
		if ch.Find(".card").Length() == 0 {
			return
		}
		fmt.Fprintf(&g.out, "<div class=%q>\n", attr(ch, "class"))
		g.walk(ch)
		g.out.WriteString("</div>\n")
	})
}

func load(dir, file string) *goquery.Document {
	f, err := os.Open(filepath.Join(dir, file))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		panic(err)
	}
	return doc
}

func main() {
	dir := os.Args[1]

	// Chrome: header, menu bar, mega menu, footer.
	home := load(dir, "d_home.html")
	var chrome strings.Builder
	chrome.WriteString("{{/* Generated by cmd/_gen_en from www.thedailystar.net: do not edit by hand. */}}\n")
	header := home.Find("header.site-header").First()
	header.Find(".header-top-news .view-content").First().SetHtml("@@HEADER_NEWS@@")
	parts := map[string]*goquery.Selection{
		"en_header":   header,
		"en_navbar":   home.Find("#site-nav-header").First(),
		"en_megamenu": home.Find(".main-nav-wrapper").First(),
		"en_footer":   home.Find("footer.site-footer").First(),
	}
	for _, name := range []string{"en_header", "en_navbar", "en_megamenu", "en_footer"} {
		s := parts[name]
		if s.Length() == 0 {
			panic("missing " + name)
		}
		clean(s)
		html := outer(s)
		html = strings.ReplaceAll(html, "@@HEADER_NEWS@@", `{{ template "en_header_news" . }}`)
		html = replaceDates(html)
		fmt.Fprintf(&chrome, "{{ define %q }}\n%s\n{{ end }}\n\n", name, html)
	}
	must(os.MkdirAll("web/en/templates", 0o755))
	must(os.WriteFile("web/en/templates/chrome.html", []byte(chrome.String()), 0o644))

	// Blocks.
	var tpl, specs, dispatch strings.Builder
	specs.WriteString("// Code generated by cmd/_gen_en from www.thedailystar.net; DO NOT EDIT.\n\npackage english\n\n// pageBlocks lists the blocks of each page in order: the template, the\n// sub-section title and how many stories its layout holds.\nvar pageBlocks = map[string][]BlockSpec{\n")
	for _, p := range pages {
		doc := load(dir, p.file)
		fmt.Fprintf(&specs, "\t%q: {\n", p.key)
		i := 0
		doc.Find("div[class*='news-section-layout-']").Each(func(_ int, blk *goquery.Selection) {
			m := layoutRe.FindStringSubmatch(attr(blk, "class"))
			if m == nil || blk.Closest("div[class*='news-section-layout-']").Get(0) != blk.Get(0) {
				return
			}
			if blk.Parent().Closest("div[class*='news-section-layout-']").Length() > 0 {
				return // nested
			}
			title := strings.Join(strings.Fields(blk.Find(".section-title").First().Text()), " ")
			if strings.Contains(strings.ToLower(title), "multimedia") {
				return // videos: the portal has none
			}
			clean(blk)
			g := &gen{}
			fmt.Fprintf(&g.out, "<div class=%q>\n", attr(blk, "class"))
			g.walk(blk)
			g.out.WriteString("</div>\n")
			key := fmt.Sprintf("en_%s_%d", p.key, i)
			fmt.Fprintf(&tpl, "{{ define %q }}\n%s{{ end }}\n\n", key, g.out.String())
			if dispatch.Len() > 0 {
				dispatch.WriteString("{{ else if ")
			} else {
				dispatch.WriteString("{{ if ")
			}
			fmt.Fprintf(&dispatch, "eq .Key %q }}{{ template %q . }}\n", key, key)
			fmt.Fprintf(&specs, "\t\t{Key: %q, Title: %q, Layout: %s, Cards: %d},\n", key, title, m[1], g.n)
			i++
		})
		specs.WriteString("\t},\n")
	}
	specs.WriteString("}\n")
	body := "{{/* Generated by cmd/_gen_en from www.thedailystar.net: do not edit by hand. Each block takes S (stories), Title and Href. */}}\n" +
		tpl.String() + "{{ define \"en_block\" }}" + dispatch.String() + "{{ end }}{{ end }}\n"
	must(os.WriteFile("web/en/templates/blocks.html", []byte(body), 0o644))
	must(os.MkdirAll("internal/english", 0o755))
	must(os.WriteFile("internal/english/blocks_gen.go", []byte(specs.String()), 0o644))
}

// replaceDates swaps the printed date ("Monday, November 16, 2026") for
// today's, filled in when the page is drawn.
func replaceDates(html string) string {
	re := regexp.MustCompile(`(Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday),\s+[A-Z][a-z]+\s+\d{1,2},\s+\d{4}`)
	return re.ReplaceAllString(html, "{{ enToday }}")
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

var _ = dateRe
