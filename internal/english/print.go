package english

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// The printed pages. The e-paper's pages are set as The Daily Star sets its
// paper: printlayouts.json holds the paper's own page plans for a run of days
// (every story, photo, quote, IN BRIEF column and jumped story, where it sits
// and how big it is, in the paper's page units), made by cmd/_gen_epl from
// the Star's e-paper. An edition takes the plans of the Star's paper of the
// same weekday: the same pages, supplements and boxes, filled with our
// stories.

//go:embed printlayouts.json
var printLayoutsJSON []byte

type printBox struct {
	X, Y, W, H int
	Kind       string   `json:"k"`  // story, photo, quote, brief, jump
	Pics       [][4]int `json:"p"`  // pictures on the page: x, y, w, h
	Heads      int      `json:"hd"` // 1 a headline, 2 with a deck
	Kicker     bool     `json:"kk"`
	Byline     bool     `json:"by"`
	Chars      int      `json:"ch"`
	To         []int    `json:"to"` // the story goes on: page number, box index
	From       []int    `json:"fr"` // a jump: from page number, box index
}

type printPage struct {
	No    int        `json:"no"`
	Title string     `json:"title"`
	W     int        `json:"w"`
	H     int        `json:"h"`
	Top   int        `json:"top"`
	Boxes []printBox `json:"boxes"`
}

type printEd struct {
	Name  string      `json:"name"`
	Pages []printPage `json:"pages"`
}

type printDay struct {
	Date    string    `json:"date"`
	Weekday int       `json:"wd"`
	Eds     []printEd `json:"eds"`
}

// embeddedPrintDays are the plans shipped with the portal (a week of the
// Star's paper), for a portal that cannot reach the Star's e-paper.
func embeddedPrintDays() []printDay {
	var v struct {
		Days []printDay `json:"days"`
	}
	if err := json.Unmarshal(printLayoutsJSON, &v); err != nil {
		panic("printlayouts.json: " + err.Error())
	}
	return v.Days
}

// printDayFor is the Star's paper an edition of day t is set like: the paper
// of that very date when its plans are known (livemap.go), else the latest
// paper of the same weekday before it.
func printDayFor(t time.Time) printDay {
	date := t.Format("2006-01-02")
	days := allPrintDays()
	var same, before []printDay
	for _, d := range days {
		if d.Date == date {
			return d
		}
		if d.Weekday == int(t.Weekday()) {
			same = append(same, d)
			if d.Date < date {
				before = append(before, d)
			}
		}
	}
	switch {
	case len(before) > 0:
		return before[len(before)-1]
	case len(same) > 0:
		return same[0]
	}
	return days[len(days)-1]
}

// edGroup is our name of one of the paper's editions: "" for the Dhaka
// Edition, else the supplement's. The weekend tabloid is left out.
func edGroup(name string) (string, bool) {
	switch name {
	case "Dhaka Edition":
		return "", true
	case "Business", "Lifestyle", "Campus", "Rising Star":
		return name, true
	}
	return "", false
}

// capacity is how many stories a page plan takes: one in each story and
// photo box, four in an IN BRIEF column, and one for each box kept for a
// story's rest from another page, set there when no story goes on to it.
func (p printPage) capacity() int {
	n := 0
	for _, b := range p.Boxes {
		switch b.Kind {
		case "story", "photo", "jump":
			n++
		case "brief":
			n += 4
		}
	}
	return n
}

// titlePlan is what a page of the paper prints, by its title: our name for
// the page, the stories that belong on it and, for pages that take any news
// when their own runs short, the rest.
type titlePlan struct {
	name  string
	match func(Article) bool
	loose func(Article) bool
}

func any_(Article) bool { return true }

func planForTitle(group, title string) titlePlan {
	switch group {
	case "Business":
		name := map[string]string{"BIZFRONT": "BizFront", "BIZBACK": "BizBack"}[title]
		if name == "" {
			name = "BizInner"
		}
		return titlePlan{name, func(a Article) bool { return in(a, "business") }, nil}
	case "Lifestyle":
		return titlePlan{"Ls", func(a Article) bool { return in(a, "lifestyle") }, nil}
	case "Campus", "Rising Star":
		return titlePlan{group, func(a Article) bool { return in(a, "youth") || in(a, "news", "education") }, nil}
	}
	city := func(a Article) bool {
		return in(a, "news", "crime-justice", "accidents-and-fires", "healthcare", "environment")
	}
	switch title {
	case "FRONT":
		return titlePlan{"Front", notOpinion, nil}
	case "NEWS":
		return titlePlan{"News", homeNews, notSupplementOrOpinion}
	case "CITY":
		return titlePlan{"City", city, homeNews}
	case "INTERNATIONAL":
		return titlePlan{"International", func(a Article) bool { return in(a, "news", "world") }, nil}
	case "EDITORIAL":
		return titlePlan{"Editorial", func(a Article) bool { return in(a, "opinion", "editorial", "views") }, nil}
	case "OPINION":
		return titlePlan{"Opinion", func(a Article) bool { return in(a, "opinion") || in(a, "slow-reads") }, nil}
	case "ART&ENT", "SHOWBIZ":
		return titlePlan{"Art&Ent", func(a Article) bool { return in(a, "culture") }, nil}
	case "SPORT":
		return titlePlan{"Sport", func(a Article) bool { return in(a, "sports") }, nil}
	case "BACK":
		return titlePlan{"Back", notSupplement, nil}
	case "LAW":
		return titlePlan{"Law", func(a Article) bool { return in(a, "ds", "law-our-rights") || in(a, "news", "crime-justice") }, homeNews}
	case "HEALTH":
		return titlePlan{"Health", func(a Article) bool { return in(a, "news", "healthcare") || in(a, "lifestyle", "health-fitness") }, homeNews}
	case "LITERATURE":
		return titlePlan{"Literature", func(a Article) bool { return in(a, "culture", "books-literature", "heritage") }, nil}
	case "BUSINESS":
		return titlePlan{"Business", func(a Article) bool { return in(a, "business") }, nil}
	case "ROUNDTABLE":
		return titlePlan{"Roundtable", func(a Article) bool { return in(a, "ds", "roundtables") || in(a, "opinion") }, nil}
	}
	// The paper's own pages of a day (DS, In Focus, Geopolitical, Weekend ...):
	// the slow reads and the Star's own features, else the day's news.
	name := strings.ToLower(title)
	if name != "" {
		name = strings.ToUpper(name[:1]) + name[1:]
	}
	return titlePlan{name, func(a Article) bool { return in(a, "slow-reads") || in(a, "ds") }, homeNews}
}

func notSupplementOrOpinion(a Article) bool { return notSupplement(a) && a.Section != "opinion" }

// layoutKey names the plan a page was set by: the paper's date, edition and
// page number.
func layoutKey(day printDay, ed, no int) string { return fmt.Sprintf("%s/%d/%d", day.Date, ed, no) }

func printPageByKey(key string) (printPage, bool) {
	var date string
	var ed, no int
	if _, err := fmt.Sscanf(strings.ReplaceAll(key, "/", " "), "%s %d %d", &date, &ed, &no); err != nil {
		return printPage{}, false
	}
	for _, d := range allPrintDays() {
		if d.Date != date || ed >= len(d.Eds) {
			continue
		}
		for _, p := range d.Eds[ed].Pages {
			if p.No == no {
				return p, true
			}
		}
	}
	return printPage{}, false
}

// keyForOld finds a plan for a page of an edition laid out before pages had
// one: the same weekday's page of that name (the n-th of that name).
func keyForOld(date, group, name string, nth int) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return ""
	}
	day := printDayFor(t)
	name = pageName(name)
	seen := 0
	for ei, e := range day.Eds {
		g, ok := edGroup(e.Name)
		if !ok || g != group {
			continue
		}
		for _, p := range e.Pages {
			if p.capacity() > 0 && planForTitle(g, p.Title).name == name {
				if seen == nth {
					return layoutKey(day, ei, p.No)
				}
				seen++
			}
		}
	}
	return ""
}

// drawnBox is one block of a drawn page: where it sits (in page units) and
// what fills it.
type drawnBox struct {
	Idx        int
	Kind       string
	X, Y, W, H int
	Pics       [][4]int // relative to the box
	Cols       int
	Heads      int
	Kicker     bool
	Byline     bool
	Lead       bool
	Light      bool // set in the light weight of the headline face
	Story      Article
	Has        bool
	Briefs     []Article
	Quote      string
	JumpKey    string   // ties a story that goes on to the box it goes on in
	JumpPage   int      // continues on page
	JumpCol    int      // in column
	FromPage   int      // a jump: from page
	Alt        *Article // a jump box's own story, set there when the rest it keeps room for fits where it began
	to         []int
}

// pageCols is how many columns of text a box is set in: the paper sets a
// box of a column and a half in one wide measure, so whole columns of about
// 150 units.
func pageCols(w int) int {
	return int(math.Max(1, math.Floor(float64(w+10)/150)))
}

// deckOf is a story's deck under its headline: the first sentence of its
// summary, never one cut short ("…").
func deckOf(summary string) string {
	s := strings.Join(strings.Fields(summary), " ")
	ss := sentences(s)
	if len(ss) == 0 {
		return ""
	}
	first := ss[0]
	// "Miranda.The joyous …": a sentence run on without a space.
	for i := 30; i+1 < len(first); i++ {
		if (first[i] == '.' || first[i] == '!' || first[i] == '?') && first[i+1] >= 'A' && first[i+1] <= 'Z' && first[i-1] >= 'a' && first[i-1] <= 'z' {
			first = first[:i+1]
			break
		}
	}
	if strings.HasSuffix(first, "...") || strings.HasSuffix(first, "…") {
		return ""
	}
	return first
}

// gridCol is the column of the page (1 to 6) a box starts in.
func gridCol(x int) int {
	c := int(math.Round(float64(x-35)/158.6)) + 1
	if c < 1 {
		c = 1
	}
	if c > 6 {
		c = 6
	}
	return c
}

// fillBoxes puts a page's stories in its plan's boxes: whole stories in the
// story boxes, biggest first, those with a photo where the plan has one; a
// photo box shows a story's photo with its caption; the shortest go in the
// IN BRIEF column; a quote box quotes a story of the page.
//
// Inside the paper's pages each story box takes a story about as long as
// the one the paper set in it (the longest box the longest story), so boxes
// are neither left half empty nor cut short; the front page keeps its lead
// in its lead box. What no box takes is returned: the page's spare stories,
// set under a story too short for its box (epaper.js).
func fillBoxes(p printPage, stories []Article) ([]drawnBox, []Article) {
	out := make([]drawnBox, len(p.Boxes))
	left := append([]Article(nil), stories...)
	byLength := p.Title != "FRONT"
	take := func(pred func(Article) bool) (Article, bool) {
		best := -1
		for i, a := range left {
			if pred(a) && (best < 0 || (byLength && textLen(a) > textLen(left[best]))) {
				best = i
				if !byLength {
					break
				}
			}
		}
		if best < 0 {
			return Article{}, false
		}
		a := left[best]
		left = append(left[:best], left[best+1:]...)
		return a, true
	}
	hasImage := func(a Article) bool { return a.ImageURL != "" }
	// The story boxes take their stories biggest first: by the text the paper
	// set in them (else their size), in the plan's order on the front page.
	order := []int{}
	for i, b := range p.Boxes {
		if b.Kind == "story" {
			order = append(order, i)
		}
	}
	if byLength {
		want := func(b printBox) int {
			if b.Chars > 0 {
				return b.Chars
			}
			return b.W * b.H / 9
		}
		sort.SliceStable(order, func(i, j int) bool { return want(p.Boxes[order[i]]) > want(p.Boxes[order[j]]) })
	}
	rank := map[int]int{}
	for r, i := range order {
		rank[i] = r
	}
	story := 0
	for i, b := range p.Boxes {
		d := drawnBox{Idx: i, Kind: b.Kind, X: b.X, Y: b.Y, W: b.W, H: b.H, Cols: pageCols(b.W),
			Heads: b.Heads, Kicker: b.Kicker, Byline: b.Byline, to: b.To}
		for _, q := range b.Pics {
			x, y := q[0]-b.X, q[1]-b.Y
			w, h := q[2], q[3]
			if x < 0 {
				w, x = w+x, 0
			}
			if y < 0 {
				h, y = h+y, 0
			}
			if x+w > b.W {
				w = b.W - x
			}
			if y+h > b.H {
				h = b.H - y
			}
			if w > 20 && h > 20 {
				d.Pics = append(d.Pics, [4]int{x, y, w, h})
			}
		}
		// A story has one photo: it takes the plan's biggest picture place;
		// the text fills the others. A news photo box keeps them all.
		if b.Kind == "story" && len(d.Pics) > 1 {
			big := d.Pics[0]
			for _, q := range d.Pics[1:] {
				if q[2]*q[3] > big[2]*big[3] {
					big = q
				}
			}
			d.Pics = [][4]int{big}
		}
		if b.Kind == "story" {
			d.Lead = story == 0
			d.Light = story%2 == 1 && story < 6
			story++
		}
		out[i] = d
	}
	for _, i := range order {
		d := &out[i]
		pred := any_
		if len(d.Pics) > 0 {
			pred = hasImage
		}
		if a, ok := take(pred); ok {
			d.Story, d.Has = a, true
		} else if a, ok := take(any_); ok {
			d.Story, d.Has = a, true
		}
	}
	for i := range out {
		if out[i].Kind == "photo" {
			if a, ok := take(hasImage); ok {
				out[i].Story, out[i].Has = a, true
			}
		}
	}
	// A box kept for a story's rest from another page has a story of its own
	// ready, should no rest come to it.
	for i := range out {
		if out[i].Kind == "jump" {
			if a, ok := take(any_); ok {
				out[i].Alt = &a
			}
		}
	}
	// The briefs and the quotes come after the stories have their places.
	for i := range out {
		switch out[i].Kind {
		case "quote":
			// The quote of the story it is set in, else of the page's first
			// story that has one.
			q := out[i]
			cands := []drawnBox{}
			for _, d := range out {
				if d.Kind == "story" && d.X <= q.X+4 && d.Y <= q.Y+4 && d.X+d.W >= q.X+q.W-4 && d.Y+d.H >= q.Y+q.H-4 {
					cands = append(cands, d)
				}
			}
			cands = append(cands, out...)
			for _, d := range cands {
				if d.Kind == "story" && d.Has && !d.Story.Brief() {
					if s := quoteOf(d.Story.Content); s != "" {
						out[i].Quote, out[i].Story, out[i].Has = s, d.Story, true
						break
					}
				}
			}
		}
	}
	// Short ones left join the IN BRIEF column, as many as it holds; the rest
	// are the page's spares.
	for i := range out {
		if out[i].Kind != "brief" {
			continue
		}
		for len(out[i].Briefs) < 6 {
			best := -1
			for j, a := range left {
				if best < 0 || textLen(a) < textLen(left[best]) {
					best = j
				}
			}
			if best < 0 {
				break
			}
			out[i].Briefs = append(out[i].Briefs, left[best])
			left = append(left[:best], left[best+1:]...)
		}
		out[i].Has = len(out[i].Briefs) > 0
	}
	return out, left
}

// textLen is how much text a story has to set: its full text, else its
// summary.
func textLen(a Article) int {
	if a.Content != "" {
		return len(tagStrip.ReplaceAllString(a.Content, ""))
	}
	return len(a.Summary)
}

// textNeed is how much text the paper set on a page, in its story and IN
// BRIEF boxes (a box's own count, else what its size holds).
func (p printPage) textNeed() int {
	n := 0
	for _, b := range p.Boxes {
		if b.Kind != "story" && b.Kind != "brief" {
			continue
		}
		if b.Chars > 0 {
			n += b.Chars
		} else {
			n += b.W * b.H / 9
		}
	}
	return n
}

// paperDate is the date as the paper prints it: "TUESDAY NOVEMBER 17, 2026".
func paperDate(t time.Time) string { return strings.ToUpper(t.Format("Monday January 2, 2006")) }

// hijriDate is the Hijri date of the front page's dateline ("JAMADIUS SANI 6,
// 1448 HIJRI"), by the arithmetical calendar, which gave the paper's date on
// the days its plans were taken.
func hijriDate(t time.Time) string {
	months := []string{"MUHARRAM", "SAFAR", "RABIUL AWAL", "RABIUS SANI", "JAMADIUL AWAL", "JAMADIUS SANI",
		"RAJAB", "SHABAN", "RAMADAN", "SHAWWAL", "ZILQAD", "ZILHAJJ"}
	y, m, d := t.Date()
	a := (14 - int(m)) / 12
	yy, mm := y+4800-a, int(m)+12*a-3
	jd := d + (153*mm+2)/5 + 365*yy + yy/4 - yy/100 + yy/400 - 32045
	l := jd - 1948440 + 10632
	n := (l - 1) / 10631
	l = l - 10631*n + 354
	j := ((10985-l)/5316)*((50*l)/17719) + (l/5670)*((43*l)/15238)
	l = l - ((30-j)/15)*((17719*j)/50) - (j/16)*((15238*j)/43) + 29
	month := (24 * l) / 709
	day := l - (709*month)/24
	year := 30*n + j - 30
	return fmt.Sprintf("%s %d, %d HIJRI", months[month-1], day, year)
}

// volNo is the front page's "VOL. XXXVI NO. 293": the volume counts the
// years since the paper's first issue (14 January 1991), the number its
// issues in the volume's year.
func volNo(t time.Time) string {
	start := time.Date(t.Year(), 1, 14, 0, 0, 0, 0, t.Location())
	if t.Before(start) {
		start = start.AddDate(-1, 0, 0)
	}
	vol := start.Year() - 1990
	no := int(t.Sub(start).Hours()/24) - 14 // less the days the paper is not out
	if no < 1 {
		no = 1
	}
	return fmt.Sprintf("VOL. %s NO. %d", roman(vol), no)
}

func roman(n int) string {
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// jumpHead is the short headline over a story's rest on a later page ("MPs
// may get up"): its first words, or all of a short one.
func jumpHead(title string) string {
	w := strings.Fields(title)
	if len(w) <= 5 {
		return title
	}
	return strings.Join(w[:3], " ")
}

// folioName is a page's title in its folio.
func folioName(title string) string {
	switch title {
	case "ART&ENT", "SHOWBIZ":
		return "ARTS & ENTERTAINMENT"
	case "CITY": // the city page's folio names the city
		return "DHAKA"
	case "LAW":
		return "LAW & OUR RIGHTS"
	case "INFOCUS":
		return "IN FOCUS"
	case "DS":
		return "DS+"
	case "BIG":
		return "THE BIG PICTURE"
	case "UNHEARD":
		return "UNHEARD VOICES"
	case "GEOPOLITICAL":
		return "GEOPOLITICAL INSIGHTS"
	}
	return title
}

// caption is a photo's caption: the story's summary, else its first
// sentence, else its headline.
func caption(a Article) string {
	if s := strings.TrimSpace(a.Summary); s != "" {
		return s
	}
	text := strings.Join(strings.Fields(tagStrip.ReplaceAllString(a.Content, " ")), " ")
	if ss := sentences(text); len(ss) > 0 {
		return ss[0]
	}
	return a.Title
}

// pagesLine is the front page's count of pages and its supplement: "16 PAGES
// PLUS LIFE STYLE: TK 15.00".
func pagesLine(pages []drawnPage) string {
	n, plus := 0, ""
	for _, p := range pages {
		switch p.Group {
		case "", "Business":
			n++
		default:
			if plus == "" {
				plus = map[string]string{"Lifestyle": "LIFE STYLE"}[p.Group]
				if plus == "" {
					plus = strings.ToUpper(p.Group)
				}
			}
		}
	}
	if plus != "" {
		return fmt.Sprintf("%d PAGES PLUS %s: TK 15.00", n, plus)
	}
	return fmt.Sprintf("%d PAGES: TK 15.00", n)
}

// teaser points the front page's readers to a story inside ("P3 | FIVE KILLED
// AFTER BUS COLLIDES …"), in the strip under the masthead; the Business
// front's nameplate has one too, with its photo.
type teaser struct {
	Code  string
	Title string
	No    int
	Image string
}

// leadOf is a page's main story: its biggest story box's.
func leadOf(p drawnPage) (Article, bool) {
	for _, b := range p.Boxes {
		if b.Kind == "story" && b.Has {
			return b.Story, true
		}
	}
	return Article{}, false
}

// frontTeasers are the strip's four: the Business front's lead (B1), then
// the leads of the inner pages readers turn to most, in page order (P3, P5,
// P11 …).
func frontTeasers(pages []drawnPage) []teaser {
	var out []teaser
	for _, p := range pages {
		if p.Group == "Business" && p.GNo == 1 {
			if a, ok := leadOf(p); ok {
				out = append(out, teaser{Code: "B1", Title: a.Title, No: p.No})
			}
			break
		}
	}
	want := map[string]int{"City": 1, "International": 2, "Sport": 3, "Art&Ent": 4, "Back": 5, "News": 6}
	var inner []drawnPage
	for _, p := range pages {
		if _, ok := leadOf(p); ok && p.Group == "" && p.GNo > 1 && want[p.Name] > 0 {
			inner = append(inner, p)
		}
	}
	sort.SliceStable(inner, func(i, j int) bool { return want[inner[i].Name] < want[inner[j].Name] })
	if n := 4 - len(out); len(inner) > n {
		inner = inner[:n]
	}
	sort.SliceStable(inner, func(i, j int) bool { return inner[i].No < inner[j].No })
	for _, p := range inner {
		a, _ := leadOf(p)
		out = append(out, teaser{Code: fmt.Sprintf("P%d", p.GNo), Title: a.Title, No: p.No})
	}
	return out
}

// bizTeaser is the box beside the Business nameplate: the lead of the
// supplement's last page, with its photo ("B4").
func bizTeaser(pages []drawnPage) *teaser {
	var t *teaser
	for _, p := range pages {
		if p.Group != "Business" || p.GNo < 2 {
			continue
		}
		if a, ok := leadOf(p); ok && a.ImageURL != "" {
			t = &teaser{Code: fmt.Sprintf("B%d", p.GNo), Title: a.Title, No: p.No, Image: a.ImageURL}
			if p.Title == "BIZBACK" {
				break
			}
		}
	}
	return t
}

// quoteOf is a story's sentence to set as a quote: one in quotation marks.
func quoteOf(content string) string {
	paras := paraRe.FindAllStringIndex(content, -1)
	if len(paras) < 3 {
		return ""
	}
	return pullSentence(content, paras)
}
