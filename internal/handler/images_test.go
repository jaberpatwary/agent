package handler

import (
	"bangla-news-aggregator/internal/domain"
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

func TestStoriesWithALostPhotoAreLeftOutOfTheListings(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ok.jpg"), []byte("x"), 0o644)
	backup := memBackup{"saved.jpg": []byte("y")}
	h := &PublicHandler{}
	h.SetPhotoCheck(dir, backup)

	mk := func(img string) domain.Article { return domain.Article{Title: img, ImageURL: img} }
	in := []domain.Article{mk("/img/ok.jpg"), mk("/img/lost.jpg"), mk("/img/saved.jpg"), mk("https://cdn.example/a.jpg"), mk("")}
	out := h.withPhotos(in, 0)
	if len(out) != 4 {
		t.Fatalf("kept %d stories, want 4 (only the lost photo is dropped): %+v", len(out), out)
	}
	for _, a := range out {
		if a.ImageURL == "/img/lost.jpg" {
			t.Fatal("the story with a lost photo was kept")
		}
	}
	if len(h.withPhotos(in, 2)) != 2 {
		t.Fatal("the cap was ignored")
	}
	// Without a check (tests, hosts without caching) nothing is dropped.
	if len((&PublicHandler{}).withPhotos(in, 0)) != 5 {
		t.Fatal("stories were dropped without a photo check")
	}
}
