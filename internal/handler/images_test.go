package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

type memBackup map[string][]byte

func (m memBackup) Save(name, _ string, data []byte) error { m[name] = data; return nil }
func (m memBackup) Load(name string) ([]byte, string, error) {
	if d, ok := m[name]; ok {
		return d, "image/png", nil
	}
	return nil, "", os.ErrNotExist
}

func TestImageFileLostOnRestartComesBackFromTheBackup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	backup := memBackup{"gone.png": []byte("PNGDATA")}
	os.WriteFile(filepath.Join(dir, "here.jpg"), []byte("JPGDATA"), 0o644)

	r := gin.New()
	r.GET("/img/*filepath", serveImages(dir, backup))
	get := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		return w
	}

	if w := get("/img/here.jpg"); w.Code != 200 || w.Body.String() != "JPGDATA" {
		t.Fatalf("file on disk: %d %q", w.Code, w.Body.String())
	}
	if w := get("/img/gone.png"); w.Code != 200 || w.Body.String() != "PNGDATA" {
		t.Fatalf("file only in the backup: %d %q", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.png")); err != nil {
		t.Fatal("the photo was not written back to disk")
	}
	if w := get("/img/never.png"); w.Code != 404 {
		t.Fatalf("unknown photo: %d", w.Code)
	}
	if w := get("/img/..%2F..%2Fetc%2Fpasswd"); w.Code != 404 {
		t.Fatalf("path escape: %d", w.Code)
	}
	if w := get("/img/.hidden"); w.Code != 404 {
		t.Fatalf("dotfile: %d", w.Code)
	}
}
