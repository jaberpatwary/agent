package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"bangla-news-aggregator/internal/agentic"
	"bangla-news-aggregator/internal/repository"
)

// SetAgent connects the agentic desk to the admin panel.
func (h *AdminHandler) SetAgent(desk *agentic.Desk, journeys repository.JourneyRepository) {
	h.desk, h.journeys = desk, journeys
}

// AgentPage shows the desk's status, limits and recent runs (/admin/agent).
func (h *AdminHandler) AgentPage(c *gin.Context) {
	c.HTML(http.StatusOK, "agent.html", gin.H{
		"Status":    h.desk.Status(),
		"Settings":  h.desk.Settings(),
		"Saved":     c.Query("saved") == "1",
		"Started":   c.Query("started"),
		"CSRFToken": c.GetString(csrfContextKey),
	})
}

// AgentSettings saves the editor-controlled limits.
func (h *AdminHandler) AgentSettings(c *gin.Context) {
	atoi := func(k string, def int) int {
		if n, err := strconv.Atoi(strings.TrimSpace(c.PostForm(k))); err == nil {
			return n
		}
		return def
	}
	cur := h.desk.Settings()
	s := agentic.DeskSettings{
		Enabled:       c.PostForm("enabled") == "on",
		Autopublish:   c.PostForm("autopublish") == "on",
		MaxTurns:      min(max(atoi("max_turns", cur.MaxTurns), 4), 40),
		StoriesPerRun: min(max(atoi("stories_per_run", cur.StoriesPerRun), 1), 5),
		StoryBudget:   min(max(atoi("story_budget", cur.StoryBudget), 20000), 2000000),
		Facts:         c.PostForm("facts"),
	}
	if err := h.desk.SaveSettings(s); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"message": "সেটিংস সংরক্ষণ করা যায়নি"})
		return
	}
	c.Redirect(http.StatusFound, "/admin/agent?saved=1")
}

// AgentRunNow starts a desk run from the admin panel.
func (h *AdminHandler) AgentRunNow(c *gin.Context) {
	id, started := h.desk.Start("admin")
	if !started {
		c.Redirect(http.StatusFound, "/admin/agent?started=busy")
		return
	}
	c.Redirect(http.StatusFound, "/admin/agent?started="+id.String())
}

// JourneyTrace shows every step the agent took on one story.
func (h *AdminHandler) JourneyTrace(c *gin.Context) {
	newsID := c.Param("id")
	j, err := h.journeys.Get(newsID)
	if err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{"message": "খবরটি পাওয়া যায়নি"})
		return
	}
	steps, _ := h.desk.Steps(newsID)
	c.HTML(http.StatusOK, "trace.html", gin.H{"J": j, "Steps": steps, "CSRFToken": c.GetString(csrfContextKey)})
}

// AnswerAgent stores the editor's answer to the agent's question and re-queues the story.
func (h *AdminHandler) AnswerAgent(c *gin.Context) {
	answer := strings.TrimSpace(c.PostForm("answer"))
	if answer == "" {
		c.Redirect(http.StatusFound, "/admin/journey/"+c.Param("id")+"/trace")
		return
	}
	if err := h.desk.AnswerQuestion(c.Param("id"), answer); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"message": "উত্তর সংরক্ষণ করা যায়নি"})
		return
	}
	c.Redirect(http.StatusFound, "/admin/journey?status=pending")
}

// recordArticleDecision turns an editor's publish/archive click into agent memory.
func (h *AdminHandler) recordArticleDecision(articleID uuid.UUID, status, reason string) {
	if h.desk == nil {
		return
	}
	action := map[string]string{"published": "published", "archived": "archived"}[status]
	if action == "" {
		return
	}
	art, err := h.articleUC.GetByID(articleID)
	if err != nil || art == nil {
		return
	}
	newsID := ""
	if j, err := h.journeys.ByArticleID(articleID); err == nil && j != nil {
		newsID = j.NewsID
	}
	h.desk.RecordEditorDecision(newsID, &articleID, action, reason, art.Title, art.SourceName)
}

func (h *AdminHandler) recordJourneyDecision(newsID, action, reason string) {
	if h.desk == nil {
		return
	}
	if j, err := h.journeys.Get(newsID); err == nil && j != nil {
		h.desk.RecordEditorDecision(newsID, j.ArticleID, action, reason, j.OriginalTitle, j.SourceName)
	}
}

// AgentRun handles POST /api/agent/run (n8n trigger, X-API-Key).
func (h *N8NHandler) AgentRun(c *gin.Context) {
	if h.desk == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agent desk not configured"})
		return
	}
	trigger := c.DefaultQuery("trigger", "n8n")
	id, started := h.desk.Start(trigger)
	status := http.StatusAccepted
	if !started {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"run_id": id, "started": started})
}

// AgentStatus handles GET /api/agent/status.
func (h *N8NHandler) AgentStatus(c *gin.Context) {
	if h.desk == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agent desk not configured"})
		return
	}
	c.JSON(http.StatusOK, h.desk.Status())
}

// SetDesk connects the agentic desk to the machine API.
func (h *N8NHandler) SetDesk(d *agentic.Desk) { h.desk = d }

// AgentStage handles POST /api/agent/stage/:name?run_id=… — one stage of a
// run, for a workflow that shows every agent as its own node. The plan stage
// opens the run and returns its id; each reply says which stage is next.
func (h *N8NHandler) AgentStage(c *gin.Context) {
	if h.desk == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agent desk not configured", "next": agentic.StageFinish})
		return
	}
	res, err := h.desk.Stage(c.Request.Context(), c.Param("name"), c.Query("run_id"), c.DefaultQuery("trigger", "n8n"))
	switch {
	case errors.Is(err, agentic.ErrRunBusy), errors.Is(err, agentic.ErrNoSuchRun):
		c.JSON(http.StatusConflict, res)
	case err != nil:
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "next": agentic.StageFinish})
	default:
		c.JSON(http.StatusOK, res)
	}
}
