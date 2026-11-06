package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	csrfCookieName = "csrf_token"
	csrfFieldName  = "_csrf"
	csrfContextKey = "csrf_token"
)

// csrfToken generates a cryptographically-random 32-byte hex string.
func csrfToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CSRFMiddleware implements the double-submit cookie pattern:
//  1. On every request (GET or POST) it ensures the browser holds a csrf_token
//     cookie; if not, it mints a fresh one.
//  2. On every state-changing method (POST, PUT, PATCH, DELETE) it verifies
//     that the hidden form field _csrf matches the cookie value.
//
// Returns HTTP 403 and aborts on a mismatch.
// The current token is stored in the Gin context under "csrf_token" so that
// handlers/templates can embed it via {{ .CSRFToken }}.
func CSRFMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// ── 1. Read or mint the cookie token ──────────────────────────────
		cookieToken, err := c.Cookie(csrfCookieName)
		if err != nil || cookieToken == "" {
			// No valid cookie yet — generate a new one.
			cookieToken, err = csrfToken()
			if err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "csrf init failed"})
				return
			}
			// SameSite=Strict, HttpOnly=false (JS must NOT read it — we use
			// the form field approach, not the header approach, so HttpOnly
			// would actually be fine, but the template needs it via context).
			secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
			c.SetSameSite(http.SameSiteStrictMode)
			c.SetCookie(csrfCookieName, cookieToken, 0, "/", "", secure, false)
		}

		// Expose the token to templates via the Gin context.
		c.Set(csrfContextKey, cookieToken)

		// ── 2. Verify on mutating methods ─────────────────────────────────
		method := c.Request.Method
		if method == http.MethodPost || method == http.MethodPut ||
			method == http.MethodPatch || method == http.MethodDelete {

			formToken := c.PostForm(csrfFieldName)
			if formToken == "" || formToken != cookieToken {
				c.HTML(http.StatusForbidden, "error.html", gin.H{
					"message": "CSRF token অবৈধ। পাতাটি রিলোড করে আবার চেষ্টা করুন।",
				})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}
