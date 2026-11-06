package handler

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// loginLimiter is a small per-process guard against repeated password guesses.
// For multi-instance deployments, enforce an additional rate limit at the edge.
type loginAttempt struct {
	count        int
	windowStart  time.Time
	blockedUntil time.Time
}
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
	max      int
	window   time.Duration
	block    time.Duration
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]*loginAttempt), max: 8, window: 10 * time.Minute, block: 15 * time.Minute}
}

func (l *loginLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()
		l.mu.Lock()
		a := l.attempts[ip]
		if a == nil || now.Sub(a.windowStart) > l.window {
			a = &loginAttempt{windowStart: now}
			l.attempts[ip] = a
		}
		blocked := now.Before(a.blockedUntil)
		if !blocked && a.count >= l.max {
			a.blockedUntil = now.Add(l.block)
			blocked = true
		}
		l.mu.Unlock()
		if blocked {
			c.Header("Retry-After", "900")
			c.String(http.StatusTooManyRequests, "Too many login attempts. Please try again later.")
			c.Abort()
			return
		}
		c.Next()
		if c.Request.Method == http.MethodPost && c.FullPath() == "/admin/login" && c.GetBool("login_failed") {
			l.mu.Lock()
			a.count++
			l.mu.Unlock()
		}
	}
}
