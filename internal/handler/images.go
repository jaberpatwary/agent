package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"bangla-news-aggregator/internal/imagestore"
)

// serveImages serves the cached photos under /img. A photo whose file is gone
// (a host that wipes its disk on restart) is brought back from the backup
// copy in the database, written to disk again, and served.
func serveImages(dir string, backup imagestore.Backup) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := filepath.Base(strings.TrimPrefix(c.Param("filepath"), "/"))
		if name == "." || name == "/" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".tmp") {
			c.Status(http.StatusNotFound)
			return
		}
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			c.Header("Cache-Control", "public, max-age=86400")
			c.File(path)
			return
		}
		if backup != nil {
			if data, contentType, err := backup.Load(name); err == nil && len(data) > 0 {
				_ = os.MkdirAll(dir, 0o755)
				_ = os.WriteFile(path, data, 0o644)
				if contentType == "" {
					contentType = http.DetectContentType(data)
				}
				c.Header("Cache-Control", "public, max-age=86400")
				c.Data(http.StatusOK, contentType, data)
				return
			}
		}
		c.Status(http.StatusNotFound)
	}
}

// photoCheck reports whether a story's photo can be served: remote and empty
// addresses pass, a cached photo passes when its file exists or can be
// brought back from the backup. The front page leaves out a story whose
// cached photo is gone, because a blank grey box (and a header card with no
// thumbnail) does not look like The Daily Star.
func photoCheck(dir string, backup imagestore.Backup) func(string) bool {
	return func(imageURL string) bool {
		if !strings.HasPrefix(imageURL, "/img/") {
			return true
		}
		name := filepath.Base(imageURL)
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return true
		}
		if backup != nil {
			if data, _, err := backup.Load(name); err == nil && len(data) > 0 {
				_ = os.MkdirAll(dir, 0o755)
				_ = os.WriteFile(path, data, 0o644)
				return true
			}
		}
		return false
	}
}
