package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"bangla-news-aggregator/internal/usecase"
)

const cookieName = "admin_token"

// AuthMiddleware protects /admin routes. It accepts the JWT either from the
// admin_token cookie (set on login, used by the server-rendered admin UI)
// or from an Authorization: Bearer header (useful for API-style access).
// On failure it redirects browser requests to the login page.
func AuthMiddleware(authUC usecase.AuthUsecase) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ""

		if cookie, err := c.Cookie(cookieName); err == nil && cookie != "" {
			token = cookie
		} else if header := c.GetHeader("Authorization"); strings.HasPrefix(header, "Bearer ") {
			token = strings.TrimPrefix(header, "Bearer ")
		}

		if token == "" {
			redirectOrUnauthorized(c)
			return
		}

		claims, err := authUC.ParseToken(token)
		if err != nil {
			redirectOrUnauthorized(c)
			return
		}

		c.Set("admin_username", claims.Username)
		c.Next()
	}
}

func redirectOrUnauthorized(c *gin.Context) {
	if strings.HasPrefix(c.ContentType(), "application/json") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.Redirect(http.StatusFound, "/admin/login")
	c.Abort()
}
