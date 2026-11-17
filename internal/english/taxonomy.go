package english

import (
	"net/url"
	"regexp"
	"strings"
)

// Sub is a sub-section of The Daily Star's English site.
type Sub struct{ Key, Label string }

// Section is a top menu entry and its sub-sections, as on www.thedailystar.net.
type Section struct {
	Key, Label string
	Subs       []Sub
}

var sections = []Section{
	{"news", "News", []Sub{{"politics", "Politics"}, {"governance", "Governance"}, {"crime-justice", "Crime and Justice"}, {"accidents-and-fires", "Accidents and Fires"}, {"technology", "Technology"}, {"education", "Education"}, {"healthcare", "Healthcare"}, {"environment", "Environment"}, {"world", "World"}}},
	{"opinion", "Opinion", []Sub{{"editorial", "Editorial"}, {"views", "Views"}, {"interviews", "Interviews"}, {"tribute", "Tribute"}}},
	{"business", "Business", []Sub{{"economy", "Economy"}, {"agriculture", "Agriculture"}, {"industries", "Industry"}, {"startups", "Startups"}, {"global-economy", "Global Economy"}}},
	{"sports", "Sports", []Sub{{"cricket", "Cricket"}, {"football", "Football"}, {"more-sports", "More Sports"}, {"tennis", "Tennis"}}},
	{"lifestyle", "Lifestyle", []Sub{{"fashion-beauty", "Fashion"}, {"relationships-family", "Relationships"}, {"health-fitness", "Health and Wellness"}, {"food-recipes", "Food and Recipe"}, {"travelogue", "Travelogue"}}},
	{"culture", "Culture", []Sub{{"entertainment", "Entertainment"}, {"books-literature", "Books and Literature"}, {"heritage", "Heritage"}, {"tv-film", "TV & Film"}, {"music", "Music"}, {"theatre-arts", "Theatre & Arts"}}},
	{"slow-reads", "Slow Reads", []Sub{{"focus", "In Focus"}, {"geopolitical-insights", "Geopolitical Insights"}, {"big-picture", "Big Picture"}, {"unheard-voices", "Unheard Voices"}, {"wisdom", "Wisdom"}}},
	{"youth", "Youth", []Sub{{"academics", "Academics"}, {"career-skills", "Career and Skills"}, {"campus-life", "Campus Life"}, {"off-campus", "Off Campus"}, {"pop-culture", "Pop Culture"}}},
	{"ds", "Ds+", []Sub{{"business-plus", "Business +"}, {"investigative-stories", "Investigative Stories"}, {"roundtables", "Roundtables"}, {"supplements", "Supplements"}, {"law-our-rights", "Law & Our Rights"}, {"my-dhaka", "My Dhaka"}}},
}

// sectionAliases: the site uses more than one address for some sections.
var sectionAliases = map[string]string{"life-living": "lifestyle"}

// subAliases maps the site's other sub-section addresses to ours.
var subAliases = map[string]string{"fashion": "fashion-beauty"}

func findSection(key string) (Section, bool) {
	if a, ok := sectionAliases[key]; ok {
		key = a
	}
	for _, s := range sections {
		if s.Key == key {
			return s, true
		}
	}
	return Section{}, false
}

func findSub(s Section, key string) (Sub, bool) {
	if a, ok := subAliases[key]; ok {
		key = a
	}
	for _, sub := range s.Subs {
		if sub.Key == key {
			return sub, true
		}
	}
	return Sub{}, false
}

// sectionLabel and subLabel name a story's place for its card ("Cricket").
func sectionLabel(key string) string {
	if s, ok := findSection(key); ok {
		return s.Label
	}
	return "News"
}

func subLabel(section, sub string) string {
	if s, ok := findSection(section); ok {
		if b, ok := findSub(s, sub); ok {
			return b.Label
		}
		return s.Label
	}
	return "News"
}

// blockSubs ties a block's title on The Daily Star's section pages to the
// sub-section it shows.
var blockSubs = map[string]string{
	"politics": "politics", "crime & justice": "crime-justice", "accidents and fires": "accidents-and-fires",
	"governance": "governance", "education": "education", "technology": "technology", "environment": "environment",
	"world": "world", "healthcare": "healthcare",
	"editorial": "editorial", "views": "views", "interviews": "interviews", "tribute": "tribute",
	"economy": "economy", "agriculture": "agriculture", "industries": "industries", "startups": "startups",
	"global economy": "global-economy",
	"cricket":        "cricket", "football": "football", "tennis": "tennis", "other sports": "more-sports",
	"fashion & beauty": "fashion-beauty", "travel": "travelogue", "food & recipes": "food-recipes",
	"health & fitness": "health-fitness",
	"in focus":         "focus", "big picture": "big-picture", "wisdom": "wisdom", "unheard voices": "unheard-voices",
	"geopolitical insights": "geopolitical-insights",
	"entertainment":         "entertainment", "books & literature": "books-literature", "heritage": "heritage",
	"tv & film": "tv-film", "music": "music", "theatre & arts": "theatre-arts",
}

// --- classifying a story ------------------------------------------------------

type pathRule struct {
	part, section, sub string
}

// pathRules read a story's place from its address at the source; the first
// match wins, so the specific ones come first.
var pathRules = []pathRule{
	{"/slow-reads/geopolitical", "slow-reads", "geopolitical-insights"},
	{"/slow-reads/big-picture", "slow-reads", "big-picture"},
	{"/slow-reads/unheard", "slow-reads", "unheard-voices"},
	{"/slow-reads/wisdom", "slow-reads", "wisdom"},
	{"/slow-reads", "slow-reads", "focus"},
	{"/youth/academics", "youth", "academics"},
	{"/youth/career", "youth", "career-skills"},
	{"/youth/campus", "youth", "campus-life"},
	{"/youth/off-campus", "youth", "off-campus"},
	{"/youth/pop", "youth", "pop-culture"},
	{"/youth", "youth", "campus-life"},
	{"/tech-startup", "news", "technology"},
	{"/editorial", "opinion", "editorial"},
	{"/interview", "opinion", "interviews"},
	{"/tribute", "opinion", "tribute"},
	{"/opinion", "opinion", "views"},
	{"/op-ed", "opinion", "views"},
	{"/views", "opinion", "views"},
	{"/column", "opinion", "views"},
	{"/cricket", "sports", "cricket"},
	{"/football", "sports", "football"},
	{"/tennis", "sports", "tennis"},
	{"/sport", "sports", ""},
	{"/agri", "business", "agriculture"},
	{"/startup", "business", "startups"},
	{"/global-economy", "business", "global-economy"},
	{"/economy/global", "business", "global-economy"},
	{"/world-economy", "business", "global-economy"},
	{"/industr", "business", "industries"},
	{"/rmg", "business", "industries"},
	{"/energy", "business", "industries"},
	{"/economy", "business", "economy"},
	{"/stock", "business", "economy"},
	{"/bank", "business", "economy"},
	{"/trade", "business", "economy"},
	{"/business", "business", ""},
	{"/books", "culture", "books-literature"},
	{"/literature", "culture", "books-literature"},
	{"/heritage", "culture", "heritage"},
	{"/music", "culture", "music"},
	{"/film", "culture", "tv-film"},
	{"/movie", "culture", "tv-film"},
	{"/theatre", "culture", "theatre-arts"},
	{"/arts", "culture", "theatre-arts"},
	{"/entertainment", "culture", "entertainment"},
	{"/showbiz", "culture", "entertainment"},
	{"/glitz", "culture", "entertainment"},
	{"/culture", "culture", ""},
	{"/lifestyle", "lifestyle", ""},
	{"/life-living", "lifestyle", ""},
	{"/life-style", "lifestyle", ""},
	{"/health", "news", "healthcare"},
	{"/education", "news", "education"},
	{"/campus", "news", "education"},
	{"/environment", "news", "environment"},
	{"/climate", "news", "environment"},
	{"/tech", "news", "technology"},
	{"/science", "news", "technology"},
	{"/world", "news", "world"},
	{"/international", "news", "world"},
	{"/asia", "news", "world"},
	{"/middle-east", "news", "world"},
	{"/europe", "news", "world"},
	{"/americas", "news", "world"},
	{"/africa", "news", "world"},
	{"/politics", "news", "politics"},
	{"/crime", "news", "crime-justice"},
	{"/court", "news", "crime-justice"},
	{"/law", "news", "crime-justice"},
	{"/accident", "news", "accidents-and-fires"},
	{"/fire", "news", "accidents-and-fires"},
	{"/features", "slow-reads", "focus"},
	{"/in-focus", "slow-reads", "focus"},
	{"/analysis", "slow-reads", "big-picture"},
	{"/long-form", "slow-reads", "big-picture"},
	{"/investigat", "slow-reads", "big-picture"},
	{"/explainer", "slow-reads", "focus"},
}

// subWords decide the sub-section from the headline when the address does not.
// Each word is matched as a whole word (with its plural and -ed/-ing forms),
// so "protest" is not a cricket "test"; a word ending in * is a stem
// ("sentenc*" for sentenced, sentencing); one ending in a space takes no
// ending at all ("us " is the country, not "used").
var subWords = map[string][][2]string{
	"news": {
		{"world", "us |u.s.|trump|china|india|pakistan|gaza|israel|iran|russia|ukraine|myanmar|rohingya repatriation|un |united nations|nepal|sri lanka|europe|uk |britain|japan|saudi|malaysia"},
		{"politics", "election|bnp|awami|jamaat|parliament|mp |party|parties|rally|ncp|polls|vote|ballot|candidate|nomination"},
		{"crime-justice", "murder|killed in|arrest|court|case|verdict|sentenc*|police|rab |detain|bail|rape|robbery|extortion|smuggl*|tribunal|jail|stabbed|hacked"},
		{"accidents-and-fires", "fire|blaze|accident|crash|collision|drown|capsiz*|electrocut*|lightning|explosion|bulkhead|sinks"},
		{"education", "university|student|exam|ssc|hsc|school|college|teacher|admission|campus"},
		{"healthcare", "hospital|dengue|health|patient|doctor|disease|vaccine|medical|chikungunya"},
		{"environment", "climate|pollution|river|forest|cyclone|weather|rain|flood|wildlife|air quality|elephant|environment"},
		{"technology", "ai |ai-|artificial intelligence|internet|mobile|app |apps |cyber|digital|software|satellite|tech*"},
	},
	"sports": {
		{"cricket", "cricket|test|odi|t20|wicket|bcb|bpl|innings|bowler|batter|icc|tigers|tigresses|century|ton "},
		{"football", "football|fifa|goal|league|bff|messi|ronaldo|uefa|saff|striker|world cup qualif*"},
		{"tennis", "tennis|grand slam|wimbledon|atp|wta"},
	},
	"business": {
		{"agriculture", "rice|crop|farmer|harvest|fish|potato|agri*|paddy|fertiliser|onion"},
		{"industries", "rmg|garment|factory|industry|power|gas|energy|steel|cement|export"},
		{"startups", "startup|fintech|entrepreneur|venture"},
		{"global-economy", "global|imf|world bank|oil price|fed |tariff|wall street|china's economy"},
	},
	"lifestyle": {
		{"fashion-beauty", "fashion|beauty|skin|hair|makeup|style|outfit|wear"},
		{"food-recipes", "recipe|food|cook|dish|restaurant|eat|cuisine|cafe"},
		{"travelogue", "travel|tour|trip|destination|holiday|beach"},
		{"health-fitness", "health|fitness|diet|exercise|sleep|wellness|weight|mental"},
	},
	"culture": {
		{"tv-film", "film|movie|drama|series|actor|actress|tv |ott|cinema|bond"},
		{"music", "song|music|concert|band|singer|album"},
		{"books-literature", "book|novel|poet|poem|literature|author|writer"},
		{"theatre-arts", "theatre|art |exhibition|painting|sculpture|dance"},
		{"heritage", "heritage|history|archaeolog*|ancient|mosque|temple"},
	},
}

// partyLeague matches the parties and their wings named "... League"; they
// are politics, never football.
var partyLeague = regexp.MustCompile(`\b(awami|chhatra|jubo|juba|swechchhasebak|krishak|sramik|mohila|muslim|tanti|matsyajibi|olama)\s+league\b`)

// subDefault is where a story goes when nothing places it more exactly.
var subDefault = map[string]string{
	"news": "governance", "sports": "more-sports", "business": "economy",
	"lifestyle": "relationships-family", "culture": "entertainment", "opinion": "views", "slow-reads": "focus",
}

// placeByPath reads a story's place from its address alone ("" if it does
// not tell).
func placeByPath(link string) (section, sub string) {
	path := strings.ToLower(link)
	if u, err := url.Parse(link); err == nil {
		path = strings.ToLower(u.Path)
	}
	for _, r := range pathRules {
		if strings.Contains(path, r.part) {
			return r.section, r.sub
		}
	}
	return "", ""
}

// classify places a story by its address at the source, then its headline.
func classify(link, title string) (section, sub string) {
	section, sub = placeByPath(link)
	head := " " + partyLeague.ReplaceAllString(strings.ToLower(title), "$1 party") + " "
	if section == "" {
		section = "news"
		// A sports or business headline from a general news address; a party,
		// police or court story stays news whatever else it mentions.
		if s := pickSub("news", head); s != "politics" && s != "crime-justice" {
			for _, s := range []string{"sports", "business"} {
				if pickSub(s, head) != "" {
					section = s
					break
				}
			}
		}
	}
	if sub == "" {
		sub = pickSub(section, head)
	}
	if sub == "" {
		sub = subDefault[section]
	}
	return section, sub
}

func pickSub(section, head string) string {
	for _, rule := range subWords[section] {
		for _, w := range strings.Split(rule[1], "|") {
			if hasWord(head, w) {
				return rule[0]
			}
		}
	}
	return ""
}

// hasWord finds the word w (as written in subWords) in the lower-case text s.
func hasWord(s, w string) bool {
	exact := strings.HasSuffix(w, " ")
	stem := strings.HasSuffix(w, "*")
	w = strings.TrimSpace(strings.TrimSuffix(w, "*"))
	if w == "" {
		return false
	}
	for from := 0; from < len(s); {
		i := strings.Index(s[from:], w)
		if i < 0 {
			return false
		}
		at, end := from+i, from+i+len(w)
		from = at + 1
		if at > 0 && wordByte(s[at-1]) && wordByte(w[0]) {
			continue // inside a word: "protest" is not "test"
		}
		if stem || !wordByte(w[len(w)-1]) {
			return true
		}
		rest := end
		for rest < len(s) && wordByte(s[rest]) {
			rest++
		}
		switch s[end:rest] {
		case "":
			return true
		case "s", "es", "d", "ed", "ing", "er", "ers":
			if !exact {
				return true
			}
		}
	}
	return false
}

func wordByte(b byte) bool { return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' }
