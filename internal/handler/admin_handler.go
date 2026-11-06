package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"bangla-news-aggregator/internal/agentic"
	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
	"bangla-news-aggregator/internal/usecase"
)

// AdminHandler serves the protected back-office UI.
type AdminHandler struct {
	authUC     usecase.AuthUsecase
	articleUC  usecase.ArticleUsecase
	feedUC     usecase.FeedUsecase
	settingsUC usecase.SettingsUsecase
	usageUC    usecase.AIUsageUsecase
	worker     *cron.Worker
	desk       *agentic.Desk
	journeys   repository.JourneyRepository
	cookieTTL  time.Duration
}

func NewAdminHandler(authUC usecase.AuthUsecase, articleUC usecase.ArticleUsecase, feedUC usecase.FeedUsecase, settingsUC usecase.SettingsUsecase, usageUC usecase.AIUsageUsecase, worker *cron.Worker, cookieTTL time.Duration) *AdminHandler {
	if cookieTTL <= 0 {
		cookieTTL = 24 * time.Hour
	}
	return &AdminHandler{authUC: authUC, articleUC: articleUC, feedUC: feedUC, settingsUC: settingsUC, usageUC: usageUC, worker: worker, cookieTTL: cookieTTL}
}

// LoginPage renders the login form.
func (h *AdminHandler) LoginPage(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", gin.H{})
}

// Login verifies credentials and sets the admin_token cookie.
func (h *AdminHandler) Login(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	token, err := h.authUC.Login(username, password)
	if err != nil {
		c.Set("login_failed", true)
		c.HTML(http.StatusUnauthorized, "login.html", gin.H{"Error": "ভুল ইউজারনেম বা পাসওয়ার্ড"})
		return
	}

	secure := os.Getenv("APP_ENV") == "production"
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(cookieName, token, int(h.cookieTTL.Seconds()), "/", "", secure, true)
	c.Redirect(http.StatusFound, "/admin/dashboard")
}

// Logout clears the admin cookie.
func (h *AdminHandler) Logout(c *gin.Context) {
	secure := os.Getenv("APP_ENV") == "production"
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(cookieName, "", -1, "/", "", secure, true)
	c.Redirect(http.StatusFound, "/admin/login")
}

// Dashboard shows summary stats and recent articles.
func (h *AdminHandler) Dashboard(c *gin.Context) {
	articles, total, _ := h.articleUC.List(1, 10, "", "")
	feeds, _ := h.feedUC.List()
	// A cheap count-only query (limit 1) just to surface how many items are
	// sitting in the approval queue — otherwise pending_review articles are
	// invisible unless an admin thinks to filter for them.
	_, pendingCount, _ := h.articleUC.List(1, 1, "", "pending_review")

	c.HTML(http.StatusOK, "dashboard.html", gin.H{
		"Articles":     articles,
		"TotalCount":   total,
		"PendingCount": pendingCount,
		"Feeds":        feeds,
		"Username":     c.GetString("admin_username"),
		"CSRFToken":    c.GetString(csrfContextKey),
	})
}

// ArticlesList shows all articles with management actions.
func (h *AdminHandler) ArticlesList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	status := c.Query("status")

	articles, total, err := h.articleUC.List(page, 20, "", status)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"message": "লোড ব্যর্থ হয়েছে"})
		return
	}

	c.HTML(http.StatusOK, "articles.html", gin.H{
		"Articles":  articles,
		"Total":     total,
		"Page":      page,
		"Status":    status,
		"CSRFToken": c.GetString(csrfContextKey),
	})
}

// DeleteArticle removes an article by ID.
func (h *AdminHandler) DeleteArticle(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.articleUC.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/admin/articles")
}

// UpdateArticleStatus publishes/archives an article.
func (h *AdminHandler) UpdateArticleStatus(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	status := c.PostForm("status")
	if err := h.articleUC.UpdateStatus(id, status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.recordArticleDecision(id, status, c.PostForm("reason"))
	if back := c.PostForm("back"); strings.HasPrefix(back, "/admin/") {
		c.Redirect(http.StatusFound, back)
		return
	}
	c.Redirect(http.StatusFound, "/admin/articles")
}

// FeedsList shows all configured RSS feeds.
func (h *AdminHandler) FeedsList(c *gin.Context) {
	feeds, err := h.feedUC.List()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"message": "লোড ব্যর্থ হয়েছে"})
		return
	}
	c.HTML(http.StatusOK, "feeds.html", gin.H{
		"Feeds":     feeds,
		"CSRFToken": c.GetString(csrfContextKey),
	})
}

// CreateFeed adds a new RSS feed source.
func (h *AdminHandler) CreateFeed(c *gin.Context) {
	name := c.PostForm("name")
	url := c.PostForm("url")
	category := c.PostForm("category")

	if name == "" || url == "" {
		c.Redirect(http.StatusFound, "/admin/feeds")
		return
	}

	if _, err := h.feedUC.Create(name, url, category); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/admin/feeds")
}

// DeleteFeed removes a feed source.
func (h *AdminHandler) DeleteFeed(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.feedUC.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/admin/feeds")
}

// ToggleFeed activates/deactivates a feed source.
func (h *AdminHandler) ToggleFeed(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	active := c.PostForm("active") == "true"
	if err := h.feedUC.SetActive(id, active); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/admin/feeds")
}

// RunCronNow starts the agentic desk right away (the dashboard's "fetch now"
// button). Falls back to the legacy fixed pipeline only if no desk is wired.
func (h *AdminHandler) RunCronNow(c *gin.Context) {
	if h.desk != nil {
		h.desk.Start("admin")
		c.Redirect(http.StatusFound, "/admin/agent?started=dashboard")
		return
	}
	// Use a background context, not the request's: the request context is
	// cancelled as soon as this handler returns, which would kill the run.
	go h.worker.RunOnce(context.Background())
	c.Redirect(http.StatusFound, "/admin/dashboard")
}

// PasswordPage renders the signed-in administrator's password form.
func (h *AdminHandler) PasswordPage(c *gin.Context) {
	c.HTML(http.StatusOK, "password.html", gin.H{
		"Username":  c.GetString("admin_username"),
		"CSRFToken": c.GetString(csrfContextKey),
	})
}

// ChangePassword verifies the current password and stores a bcrypt hash.
func (h *AdminHandler) ChangePassword(c *gin.Context) {
	current := c.PostForm("current_password")
	newPassword := c.PostForm("new_password")
	confirm := c.PostForm("confirm_password")
	csrfTok := c.GetString(csrfContextKey)
	if newPassword != confirm {
		c.HTML(http.StatusBadRequest, "password.html", gin.H{"Error": "নতুন পাসওয়ার্ড দুটি মেলেনি", "Username": c.GetString("admin_username"), "CSRFToken": csrfTok})
		return
	}
	if err := h.authUC.ChangePassword(c.GetString("admin_username"), current, newPassword); err != nil {
		message := "পাসওয়ার্ড পরিবর্তন করা যায়নি। বর্তমান পাসওয়ার্ড যাচাই করুন এবং নতুন পাসওয়ার্ড অন্তত ১২ অক্ষরের দিন।"
		c.HTML(http.StatusBadRequest, "password.html", gin.H{"Error": message, "Username": c.GetString("admin_username"), "CSRFToken": csrfTok})
		return
	}
	c.HTML(http.StatusOK, "password.html", gin.H{"Success": "পাসওয়ার্ড সফলভাবে পরিবর্তন হয়েছে।", "Username": c.GetString("admin_username"), "CSRFToken": csrfTok})
}

// AIUsagePage shows recent AI provider activity: per-provider success rate,
// token usage, and the latest individual calls (draft + review attempts).
func (h *AdminHandler) AIUsagePage(c *gin.Context) {
	overall, recent, err := h.usageUC.RecentStats(500)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"message": "AI ব্যবহারের তথ্য লোড করা যায়নি"})
		return
	}
	c.HTML(http.StatusOK, "ai_usage.html", gin.H{
		"Overall":  overall,
		"Recent":   recent,
		"Username": c.GetString("admin_username"),
	})
}

// StylePage renders the AI rewrite house-style editor. Changing this text
// changes how the next cron run's agentic rewriter writes new articles —
// no redeploy needed.
func (h *AdminHandler) StylePage(c *gin.Context) {
	style, err := h.settingsUC.GetHouseStyle()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"message": "স্টাইল লোড করা যায়নি"})
		return
	}
	sensitive, err := h.settingsUC.GetSensitiveCategories()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"message": "সেটিংস লোড করা যায়নি"})
		return
	}
	c.HTML(http.StatusOK, "style.html", gin.H{
		"Style":        style,
		"SensitiveCSV": strings.Join(sensitive, ", "),
		"Username":     c.GetString("admin_username"),
		"CSRFToken":    c.GetString(csrfContextKey),
	})
}

// UpdateStyle saves new house-style instructions and the sensitive-category
// list for the agentic rewriter.
func (h *AdminHandler) UpdateStyle(c *gin.Context) {
	style := c.PostForm("style")
	sensitiveRaw := c.PostForm("sensitive_categories")
	csrfTok := c.GetString(csrfContextKey)

	if err := h.settingsUC.SetHouseStyle(style); err != nil {
		c.HTML(http.StatusBadRequest, "style.html", gin.H{
			"Error": err.Error(), "Style": style, "SensitiveCSV": sensitiveRaw,
			"Username": c.GetString("admin_username"), "CSRFToken": csrfTok,
		})
		return
	}
	if err := h.settingsUC.SetSensitiveCategories(strings.Split(sensitiveRaw, ",")); err != nil {
		c.HTML(http.StatusBadRequest, "style.html", gin.H{
			"Error": err.Error(), "Style": style, "SensitiveCSV": sensitiveRaw,
			"Username": c.GetString("admin_username"), "CSRFToken": csrfTok,
		})
		return
	}
	c.HTML(http.StatusOK, "style.html", gin.H{
		"Success": "সেটিংস সংরক্ষণ করা হয়েছে। পরবর্তী ক্রন রান থেকে এটি নতুন খবরে প্রয়োগ হবে।",
		"Style":   style, "SensitiveCSV": sensitiveRaw,
		"Username": c.GetString("admin_username"), "CSRFToken": csrfTok,
	})
}

// JourneyRow is one story on /admin/journey, with its cross-check sources decoded.
type JourneyRow struct {
	domain.NewsJourney
	Sources []cron.JourneySource
}

// JourneyPage shows every story's trip through the n8n pipeline.
func (h *AdminHandler) JourneyPage(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	status := c.Query("status")
	rows, total, err := h.worker.ListJourneys(status, page, 30)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"message": "লোড ব্যর্থ হয়েছে"})
		return
	}
	view := make([]JourneyRow, 0, len(rows))
	for _, r := range rows {
		jr := JourneyRow{NewsJourney: r}
		if r.CrossCheckSources != "" {
			_ = json.Unmarshal([]byte(r.CrossCheckSources), &jr.Sources)
		}
		view = append(view, jr)
	}
	c.HTML(http.StatusOK, "journey.html", gin.H{
		"Rows":      view,
		"Total":     total,
		"Page":      page,
		"HasNext":   int64(page*30) < total,
		"Status":    status,
		"CSRFToken": c.GetString(csrfContextKey),
	})
}

// RejectJourney confirms the AI's "suspected fake" verdict.
func (h *AdminHandler) RejectJourney(c *gin.Context) {
	if err := h.worker.RejectJourney(c.Param("id")); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"message": "বাতিল করা যায়নি"})
		return
	}
	h.recordJourneyDecision(c.Param("id"), "rejected", c.PostForm("reason"))
	c.Redirect(http.StatusFound, "/admin/journey?status=suspected_fake")
}

// RequeueJourney overrules a "fake" verdict and sends the story back into the queue.
func (h *AdminHandler) RequeueJourney(c *gin.Context) {
	if err := h.worker.RequeueJourney(c.Param("id")); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"message": "আবার চালু করা যায়নি"})
		return
	}
	h.recordJourneyDecision(c.Param("id"), "requeued", c.PostForm("reason"))
	c.Redirect(http.StatusFound, "/admin/journey")
}
