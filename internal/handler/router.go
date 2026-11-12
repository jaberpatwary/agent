package handler

import (
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gin-gonic/gin"
	xhtml "golang.org/x/net/html"

	"bangla-news-aggregator/internal/usecase"
)

var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

// sanitizeArticleHTML keeps basic article formatting while removing active
// content from untrusted RSS markup before it is rendered as HTML.
func sanitizeArticleHTML(input string) template.HTML {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(input))
	if err != nil {
		return template.HTML(template.HTMLEscapeString(input))
	}

	doc.Find("script,style,iframe,object,embed,form,svg,math,link,meta,base").Remove()
	allowedTags := map[string]bool{
		"p": true, "br": true, "div": true, "span": true, "h1": true, "h2": true,
		"h3": true, "h4": true, "h5": true, "h6": true, "blockquote": true,
		"ul": true, "ol": true, "li": true, "strong": true, "b": true, "em": true,
		"i": true, "u": true, "s": true, "a": true, "img": true, "figure": true,
		"figcaption": true, "table": true, "thead": true, "tbody": true,
		"tr": true, "th": true, "td": true, "hr": true, "pre": true, "code": true,
	}
	doc.Find("body *").Each(func(_ int, selection *goquery.Selection) {
		node := selection.Get(0)
		if !allowedTags[node.Data] {
			selection.ReplaceWithSelection(selection.Contents())
			return
		}
		attrs := append([]xhtml.Attribute(nil), node.Attr...)
		node.Attr = nil
		for _, attr := range attrs {
			name := strings.ToLower(attr.Key)
			if strings.HasPrefix(name, "on") || name == "style" || name == "srcdoc" {
				continue
			}
			switch name {
			case "href", "src":
				u := strings.TrimSpace(attr.Val)
				if strings.ContainsAny(u, "\x00\r\n\t") {
					continue
				}
				parsed, parseErr := url.Parse(u)
				if parseErr != nil {
					continue
				}
				scheme := strings.ToLower(parsed.Scheme)
				if scheme != "" && scheme != "http" && scheme != "https" && !(name == "href" && scheme == "mailto") {
					continue
				}
			case "alt", "title", "width", "height", "colspan", "rowspan":
			default:
				continue
			}
			node.Attr = append(node.Attr, attr)
		}
	})
	clean, err := doc.Find("body").Html()
	if err != nil {
		return template.HTML(template.HTMLEscapeString(input))
	}
	return template.HTML(clean)
}

func stripHTML(input string) string {
	cleaned := htmlTagRegex.ReplaceAllString(input, " ")
	if idx := strings.LastIndex(cleaned, "<"); idx != -1 {
		if !strings.Contains(cleaned[idx:], ">") {
			cleaned = cleaned[:idx]
		}
	}
	cleaned = html.UnescapeString(cleaned)
	return strings.TrimSpace(strings.Join(strings.Fields(cleaned), " "))
}

func categoryLabel(category string) string {
	switch category {
	case "bangladesh":
		return "বাংলাদেশ"
	case "international":
		return "বিশ্ব"
	case "business":
		return "ব্যবসা"
	case "sports":
		return "খেলা"
	case "entertainment":
		return "বিনোদন"
	case "technology":
		return "প্রযুক্তি"
	case "general":
		return "সাধারণ"
	default:
		return category
	}
}

func toBanglaDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r - '0' + '০')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func bangla12HourTime(t time.Time) string {
	loc, err := time.LoadLocation("Asia/Dhaka")
	if err == nil {
		t = t.In(loc)
	}
	hour := t.Hour()
	minute := t.Minute()

	var period string
	switch {
	case hour >= 4 && hour < 6:
		period = "ভোর"
	case hour >= 6 && hour < 12:
		period = "সকাল"
	case hour >= 12 && hour < 15:
		period = "দুপুর"
	case hour >= 15 && hour < 18:
		period = "বিকাল"
	case hour >= 18 && hour < 20:
		period = "সন্ধ্যা"
	default:
		period = "রাত"
	}

	h12 := hour % 12
	if h12 == 0 {
		h12 = 12
	}

	return fmt.Sprintf("%s %s:%s", period, toBanglaDigits(fmt.Sprintf("%02d", h12)), toBanglaDigits(fmt.Sprintf("%02d", minute)))
}

func banglaDateTime(t time.Time) string {
	loc, err := time.LoadLocation("Asia/Dhaka")
	if err == nil {
		t = t.In(loc)
	}
	bengaliMonths := []string{
		"", "জানুয়ারি", "ফেব্রুয়ারি", "মার্চ", "এপ্রিল", "মে", "জুন",
		"জুলাই", "আগস্ট", "সেপ্টেম্বর", "অক্টোবর", "নভেম্বর", "ডিসেম্বর",
	}
	monthName := bengaliMonths[t.Month()]
	return fmt.Sprintf("%s %s %s, %s", toBanglaDigits(t.Format("02")), monthName, toBanglaDigits(t.Format("2006")), bangla12HourTime(t))
}

func banglaFullDateTime(t time.Time) string {
	loc, err := time.LoadLocation("Asia/Dhaka")
	if err == nil {
		t = t.In(loc)
	}
	bengaliDays := []string{
		"রবিবার", "সোমবার", "মঙ্গলবার", "বুধবার", "বৃহস্পতিবার", "শুক্রবার", "শনিবার",
	}
	bengaliMonths := []string{
		"", "জানুয়ারি", "ফেব্রুয়ারি", "মার্চ", "এপ্রিল", "মে", "জুন",
		"জুলাই", "আগস্ট", "সেপ্টেম্বর", "অক্টোবর", "নভেম্বর", "ডিসেম্বর",
	}
	dayName := bengaliDays[t.Weekday()]
	monthName := bengaliMonths[t.Month()]
	return fmt.Sprintf("%s, %s %s %s, %s", dayName, toBanglaDigits(t.Format("02")), monthName, toBanglaDigits(t.Format("2006")), bangla12HourTime(t))
}

func banglaTimeAgo(t time.Time) string {
	loc, err := time.LoadLocation("Asia/Dhaka")
	if err == nil {
		t = t.In(loc)
	}
	now := time.Now().In(t.Location())
	diff := now.Sub(t)
	if diff < 0 {
		diff = 0
	}

	minutes := int(diff.Minutes())
	hours := int(diff.Hours())
	days := hours / 24

	if minutes < 1 {
		return "এইমাত্র"
	} else if minutes < 60 {
		return fmt.Sprintf("%s মিনিট আগে", toBanglaDigits(strconv.Itoa(minutes)))
	} else if hours < 24 {
		return fmt.Sprintf("%s ঘণ্টা আগে", toBanglaDigits(strconv.Itoa(hours)))
	} else if days < 7 {
		return fmt.Sprintf("%s দিন আগে", toBanglaDigits(strconv.Itoa(days)))
	}
	return banglaDateTime(t)
}

func todayBanglaDate() string {
	loc, err := time.LoadLocation("Asia/Dhaka")
	now := time.Now()
	if err == nil {
		now = now.In(loc)
	}
	bengaliDays := []string{
		"রবিবার", "সোমবার", "মঙ্গলবার", "বুধবার", "বৃহস্পতিবার", "শুক্রবার", "শনিবার",
	}
	bengaliMonths := []string{
		"", "জানুয়ারি", "ফেব্রুয়ারি", "মার্চ", "এপ্রিল", "মে", "জুন",
		"জুলাই", "আগস্ট", "সেপ্টেম্বর", "অক্টোবর", "নভেম্বর", "ডিসেম্বর",
	}
	return fmt.Sprintf("%s, %s %s %s", bengaliDays[now.Weekday()], toBanglaDigits(now.Format("02")), bengaliMonths[now.Month()], toBanglaDigits(now.Format("2006")))
}

// NewRouter builds the full Gin engine: templates, static files, public
// routes, and JWT-protected admin routes.
func NewRouter(
	authUC usecase.AuthUsecase,
	publicH *PublicHandler,
	adminH *AdminHandler,
	n8nH *N8NHandler, // nil disables the /api/n8n routes
	imageStorageDir string,
) *gin.Engine {
	r := gin.Default()
	r.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	})

	r.SetFuncMap(template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b int) int { return a * b },
		"div": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"safeHTML":           sanitizeArticleHTML,
		"stripHTML":          stripHTML,
		"categoryLabel":      categoryLabel,
		"banglaDigits":       toBanglaDigits,
		"banglaDate":         banglaDateTime,
		"banglaDateTime":     banglaDateTime,
		"banglaFullDateTime": banglaFullDateTime,
		"banglaTimeAgo":      banglaTimeAgo,
		"todayBanglaDate":    todayBanglaDate,
		"dhakaClock":         dhakaClock,
		"readMinutes":        readMinutes,
		"banglaCalendar":     banglaCalendarDate,
		"now":                time.Now,
	})
	r.LoadHTMLGlob("web/templates/**/*.html")
	r.Static("/static", "web/static")
	if imageStorageDir != "" {
		// Serves cached article hero images back from this portal's own
		// domain (see internal/imagestore) instead of hotlinking publishers.
		r.Static("/img", imageStorageDir)
	}

	r.NoRoute(func(c *gin.Context) {
		c.HTML(http.StatusNotFound, "error.html", gin.H{"message": "পাতাটি খুঁজে পাওয়া যায়নি"})
	})

	// ---- Public routes ----
	r.GET("/", publicH.Home)
	r.GET("/article/:id", publicH.ArticleDetail)
	r.GET("/health", publicH.HealthCheck)
	r.GET("/api/latest-check", publicH.LatestCheck)
	r.GET("/sitemap.xml", publicH.Sitemap)
	r.GET("/sitemap-news.xml", publicH.NewsSitemap)
	r.GET("/robots.txt", publicH.Robots)
	r.GET("/api/suggest", publicH.Suggest)
	r.GET("/saved", publicH.Saved)
	// PWA: the service worker must be served from the site root so its scope covers every page.
	r.GET("/sw.js", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		c.Header("Service-Worker-Allowed", "/")
		c.File("web/static/sw.js")
	})
	r.GET("/manifest.webmanifest", func(c *gin.Context) {
		c.Header("Content-Type", "application/manifest+json")
		c.File("web/static/manifest.webmanifest")
	})

	// ---- n8n machine API (enabled only when N8N_API_KEY is set) ----
	if n8nH != nil {
		n8n := r.Group("/api/n8n", n8nH.RequireAPIKey())
		n8n.GET("/pending", n8nH.Pending)
		n8n.POST("/articles", n8nH.CreateArticle)
		n8n.PUT("/journey", n8nH.UpdateJourney)
		n8n.POST("/ai", n8nH.AI)
		agentAPI := r.Group("/api/agent", n8nH.RequireAPIKey())
		agentAPI.POST("/run", n8nH.AgentRun)
		agentAPI.GET("/status", n8nH.AgentStatus)
		agentAPI.POST("/stage/:name", n8nH.AgentStage)
	}

	// ---- Admin auth routes (unprotected) ----
	admin := r.Group("/admin")
	admin.GET("/login", adminH.LoginPage)
	admin.POST("/login", newLoginLimiter().Middleware(), adminH.Login)

	// ---- Admin protected routes ----
	protected := admin.Group("")
	protected.Use(AuthMiddleware(authUC))
	protected.Use(CSRFMiddleware())
	{
		protected.GET("/dashboard", adminH.Dashboard)
		protected.GET("/password", adminH.PasswordPage)
		protected.POST("/password", adminH.ChangePassword)
		protected.GET("/style", adminH.StylePage)
		protected.POST("/style", adminH.UpdateStyle)
		protected.GET("/ai-usage", adminH.AIUsagePage)
		protected.GET("/journey", adminH.JourneyPage)
		protected.POST("/journey/:id/requeue", adminH.RequeueJourney)
		protected.POST("/journey/:id/reject", adminH.RejectJourney)
		protected.GET("/journey/:id/trace", adminH.JourneyTrace)
		protected.POST("/journey/:id/answer", adminH.AnswerAgent)
		protected.GET("/agent", adminH.AgentPage)
		protected.POST("/agent/settings", adminH.AgentSettings)
		protected.POST("/agent/run", adminH.AgentRunNow)
		protected.POST("/cron/run", adminH.RunCronNow)

		protected.GET("/articles", adminH.ArticlesList)
		protected.POST("/articles/:id/delete", adminH.DeleteArticle)
		protected.POST("/articles/:id/status", adminH.UpdateArticleStatus)

		protected.GET("/feeds", adminH.FeedsList)
		protected.POST("/feeds", adminH.CreateFeed)
		protected.POST("/feeds/:id/delete", adminH.DeleteFeed)
		protected.POST("/feeds/:id/toggle", adminH.ToggleFeed)

		protected.POST("/logout", adminH.Logout)
	}

	return r
}

// dhakaClock formats a time.Time or *time.Time in Bangladesh time; nil/zero gives "".
func dhakaClock(v any, layout string) string {
	var t time.Time
	switch x := v.(type) {
	case time.Time:
		t = x
	case *time.Time:
		if x == nil {
			return ""
		}
		t = *x
	default:
		return ""
	}
	if t.IsZero() {
		return ""
	}
	if loc, err := time.LoadLocation("Asia/Dhaka"); err == nil {
		t = t.In(loc)
	}
	return t.Format(layout)
}

// readMinutes estimates reading time for an HTML body (~200 Bangla words a minute).
func readMinutes(htmlBody string) int {
	words := len(strings.Fields(stripHTML(htmlBody)))
	m := (words + 199) / 200
	if m < 1 {
		m = 1
	}
	return m
}
