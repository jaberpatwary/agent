package handler

import (
	"encoding/json"
	"encoding/xml"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/usecase"
)

// PublicHandler serves the reader-facing pages: homepage, category feeds,
// and individual article pages.
type PublicHandler struct {
	articleUC usecase.ArticleUsecase
	baseURL   string // may be empty if PUBLIC_BASE_URL isn't configured; callers fall back to relative URLs
}

func NewPublicHandler(articleUC usecase.ArticleUsecase, baseURL string) *PublicHandler {
	return &PublicHandler{articleUC: articleUC, baseURL: strings.TrimRight(baseURL, "/")}
}

// homeSection is one category block on the front page.
type homeSection struct {
	Key      string
	Label    string
	Articles []domain.Article
}

var homeSectionOrder = []string{"bangladesh", "international", "sports", "business", "entertainment", "technology"}

// Home renders a paginated list of the latest published articles.
func (h *PublicHandler) Home(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	category := c.Query("category")
	query := c.Query("q")

	var articles []domain.Article
	var total int64
	var err error
	if query != "" {
		articles, total, err = h.articleUC.Search(page, 20, query, category, "published")
	} else {
		articles, total, err = h.articleUC.List(page, 20, category, "published")
	}
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"message": "নিবন্ধ লোড করা যায়নি"})
		return
	}

	// The front page (no filter, first page) also gets one block per section,
	// a "most read" list and the trending topics.
	var sections []homeSection
	var mostRead []domain.Article
	var trending []string
	if page <= 1 && category == "" && query == "" {
		for _, key := range homeSectionOrder {
			list, _, err := h.articleUC.List(1, 5, key, "published")
			if err == nil && len(list) > 0 {
				sections = append(sections, homeSection{Key: key, Label: categoryLabel(key), Articles: list})
			}
		}
		mostRead, _ = h.articleUC.MostRead(6)
		trending, _ = h.articleUC.TrendingTags(12)
	}

	c.HTML(http.StatusOK, "index.html", gin.H{
		"Sections":     sections,
		"MostRead":     mostRead,
		"Trending":     trending,
		"Articles":     articles,
		"Total":        total,
		"Page":         page,
		"Category":     category,
		"Query":        query,
		"HasMore":      int64(page*20) < total,
		"CanonicalURL": h.absoluteURL("/"),
		"BaseURL":      h.baseURL,
	})
}

// ArticleDetail renders a single article by ID.
func (h *PublicHandler) ArticleDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{"message": "নিবন্ধ খুঁজে পাওয়া যায়নি"})
		return
	}

	article, err := h.articleUC.GetByID(id)
	if err != nil || article == nil || article.Status != "published" {
		c.HTML(http.StatusNotFound, "error.html", gin.H{"message": "নিবন্ধ খুঁজে পাওয়া যায়নি"})
		return
	}

	h.articleUC.RecordView(article.ID)

	related, err := h.articleUC.Related(article, 4)
	if err != nil {
		// Related stories are a nice-to-have, not essential to the page —
		// log and continue rather than failing the whole article view.
		related = nil
	}

	var tags []string
	for _, t := range strings.Split(article.Tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}

	canonicalURL := h.absoluteURL("/article/" + article.ID.String())
	c.HTML(http.StatusOK, "article.html", gin.H{
		"Article":      article,
		"Category":     article.Category,
		"Tags":         tags,
		"Related":      related,
		"CanonicalURL": canonicalURL,
		"BaseURL":      h.baseURL,
		"JSONLD":       newsArticleJSONLD(article, canonicalURL),
	})
}

// newsArticleJSONLD builds the schema.org NewsArticle structured-data block
// Google News/Discover look for. Marshaled server-side (see the comment in
// article.html) rather than interpolated field-by-field in the template.
type jsonLDPerson struct {
	Type string `json:"@type"`
	Name string `json:"name"`
}

// authorList credits the portal's own news desk. The originating outlet is
// deliberately not exposed on public pages (it stays in the admin panel).
func authorList(_ *domain.Article) []jsonLDPerson {
	return []jsonLDPerson{{Type: "Organization", Name: "বাংলা নিউজ ডেস্ক"}}
}

func newsArticleJSONLD(a *domain.Article, canonicalURL string) template.JS {
	doc := struct {
		Context       string         `json:"@context"`
		Type          string         `json:"@type"`
		Headline      string         `json:"headline"`
		Description   string         `json:"description,omitempty"`
		Image         []string       `json:"image,omitempty"`
		DatePublished string         `json:"datePublished"`
		DateModified  string         `json:"dateModified"`
		Author        []jsonLDPerson `json:"author"`
		Publisher     jsonLDPerson   `json:"publisher"`
		MainEntity    *struct {
			Type string `json:"@type"`
			ID   string `json:"@id"`
		} `json:"mainEntityOfPage,omitempty"`
	}{
		Context:       "https://schema.org",
		Type:          "NewsArticle",
		Headline:      a.Title,
		Description:   stripHTML(a.Summary),
		DatePublished: a.PublishedAt.Format(time.RFC3339),
		DateModified:  a.UpdatedAt.Format(time.RFC3339),
		Author:        authorList(a),
		Publisher:     jsonLDPerson{Type: "Organization", Name: "বাংলা নিউজ"},
	}
	if a.ImageURL != "" {
		doc.Image = []string{a.ImageURL}
	}
	if canonicalURL != "" {
		doc.MainEntity = &struct {
			Type string `json:"@type"`
			ID   string `json:"@id"`
		}{Type: "WebPage", ID: canonicalURL}
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		return ""
	}
	// json.Marshal already escapes everything that matters inside a
	// <script> element (HTML-special bytes are escaped by default), so this
	// is safe to mark as trusted JS and emit verbatim.
	return template.JS(raw)
}

// Suggest handles GET /api/suggest?q= — headline suggestions for the search box.
func (h *PublicHandler) Suggest(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if len([]rune(q)) < 2 {
		c.JSON(http.StatusOK, gin.H{"items": []any{}})
		return
	}
	articles, _, err := h.articleUC.Search(1, 6, q, "", "published")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"items": []any{}})
		return
	}
	type suggestion struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Image    string `json:"image"`
		Category string `json:"category"`
		Ago      string `json:"ago"`
	}
	items := make([]suggestion, 0, len(articles))
	for _, a := range articles {
		items = append(items, suggestion{ID: a.ID.String(), Title: a.Title, Image: a.ImageURL, Category: categoryLabel(a.Category), Ago: banglaTimeAgo(a.PublishedAt)})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// Saved renders the reader's bookmarks page; the list itself lives in the
// browser (localStorage) and is drawn client-side.
func (h *PublicHandler) Saved(c *gin.Context) {
	c.HTML(http.StatusOK, "saved.html", gin.H{"Category": "__saved"})
}

// HealthCheck is a simple liveness probe for Render.com / uptime monitors.
func (h *PublicHandler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// absoluteURL joins the configured public base URL with a path. If no base
// URL is configured it returns the relative path unchanged — templates then
// simply omit the tags that require an absolute URL (see article.html).
func (h *PublicHandler) absoluteURL(path string) string {
	if h.baseURL == "" {
		return path
	}
	return h.baseURL + path
}

// sitemapURLSet / sitemapURL model the minimal sitemap.xml schema
// (https://www.sitemaps.org/protocol.html) that search engines expect.
type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// Sitemap serves /sitemap.xml so search engines can discover every published
// article without crawling pagination. Requires PUBLIC_BASE_URL to be set;
// without it there is no valid absolute URL to advertise, so it 503s with an
// explanation rather than emitting a sitemap full of relative paths that
// search engines would reject anyway.
func (h *PublicHandler) Sitemap(c *gin.Context) {
	if h.baseURL == "" {
		c.String(http.StatusServiceUnavailable, "sitemap.xml requires PUBLIC_BASE_URL to be configured")
		return
	}
	// 1000 most recent published articles is a reasonable cap for a single
	// sitemap file; a site this size doesn't need sitemap index/pagination.
	articles, _, err := h.articleUC.List(1, 1000, "", "published")
	if err != nil {
		c.String(http.StatusInternalServerError, "could not load articles")
		return
	}
	urlSet := sitemapURLSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	urlSet.URLs = append(urlSet.URLs, sitemapURL{Loc: h.absoluteURL("/"), LastMod: time.Now().Format("2006-01-02")})
	for _, a := range articles {
		urlSet.URLs = append(urlSet.URLs, sitemapURL{
			Loc:     h.absoluteURL("/article/" + a.ID.String()),
			LastMod: a.UpdatedAt.Format("2006-01-02"),
		})
	}
	c.Header("Content-Type", "application/xml; charset=utf-8")
	c.XML(http.StatusOK, urlSet)
}

// Google News Sitemap XML types: https://developers.google.com/search/docs/crawling-indexing/sitemaps/news-sitemap
// This is a DIFFERENT schema from the general /sitemap.xml above — it needs
// the news: namespace and only covers articles from the last 48 hours
// (Google ignores older entries in this particular file; they're still
// discoverable via the general sitemap and normal crawling).
type newsSitemapURLSet struct {
	XMLName xml.Name         `xml:"urlset"`
	Xmlns   string           `xml:"xmlns,attr"`
	News    string           `xml:"xmlns:news,attr"`
	URLs    []newsSitemapURL `xml:"url"`
}

type newsSitemapURL struct {
	Loc  string             `xml:"loc"`
	News newsSitemapArticle `xml:"news:news"`
}

type newsSitemapArticle struct {
	Publication     newsSitemapPublication `xml:"news:publication"`
	PublicationDate string                 `xml:"news:publication_date"`
	Title           string                 `xml:"news:title"`
}

type newsSitemapPublication struct {
	Name     string `xml:"news:name"`
	Language string `xml:"news:language"`
}

// NewsSitemap serves /sitemap-news.xml for Google News discovery, separate
// from the general /sitemap.xml. Same PUBLIC_BASE_URL requirement.
func (h *PublicHandler) NewsSitemap(c *gin.Context) {
	if h.baseURL == "" {
		c.String(http.StatusServiceUnavailable, "sitemap-news.xml requires PUBLIC_BASE_URL to be configured")
		return
	}
	// Google News sitemaps are capped at 1000 URLs and should only list
	// recent articles; we fetch generously and filter to the last 48 hours.
	articles, _, err := h.articleUC.List(1, 1000, "", "published")
	if err != nil {
		c.String(http.StatusInternalServerError, "could not load articles")
		return
	}
	cutoff := time.Now().Add(-48 * time.Hour)
	urlSet := newsSitemapURLSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		News:  "http://www.google.com/schemas/sitemap-news/0.9",
	}
	for _, a := range articles {
		if a.PublishedAt.Before(cutoff) {
			continue
		}
		urlSet.URLs = append(urlSet.URLs, newsSitemapURL{
			Loc: h.absoluteURL("/article/" + a.ID.String()),
			News: newsSitemapArticle{
				Publication:     newsSitemapPublication{Name: "বাংলা নিউজ", Language: "bn"},
				PublicationDate: a.PublishedAt.Format(time.RFC3339),
				Title:           a.Title,
			},
		})
	}
	c.Header("Content-Type", "application/xml; charset=utf-8")
	c.XML(http.StatusOK, urlSet)
}

// Robots serves /robots.txt, pointing crawlers at the sitemap when one is
// available.
func (h *PublicHandler) Robots(c *gin.Context) {
	body := "User-agent: *\nAllow: /\nDisallow: /admin/\n"
	if h.baseURL != "" {
		body += "Sitemap: " + h.baseURL + "/sitemap.xml\n"
		body += "Sitemap: " + h.baseURL + "/sitemap-news.xml\n"
	}
	c.String(http.StatusOK, body)
}

// LatestCheck returns the ID, title, and count of latest published articles for auto-update checks.
func (h *PublicHandler) LatestCheck(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	category := c.Query("category")
	query := c.Query("q")
	var articles []domain.Article
	var total int64
	var err error
	if query != "" {
		articles, total, err = h.articleUC.Search(page, 1, query, category, "published")
	} else {
		articles, total, err = h.articleUC.List(page, 1, category, "published")
	}
	if err != nil || len(articles) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"total":        total,
			"latest_id":    "",
			"latest_title": "",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"total":        total,
		"latest_id":    articles[0].ID.String(),
		"latest_title": articles[0].Title,
		"published_at": articles[0].PublishedAt,
	})
}
