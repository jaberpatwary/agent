//go:build ignore

// Command cache_published_images makes every published article serve its
// photo from this portal's own image store (/img/...):
//
//   - remote image URLs are downloaded and swapped for the local copy;
//   - local paths whose file is missing (e.g. lost before the store had a
//     persistent volume) are re-found by scraping the source page;
//   - articles for which no usable photo can be found are archived, because
//     the portal only shows stories that have a photo. Archiving is
//     reversible from /admin/articles.
//
// Run inside the compose network with the image volume mounted, e.g.:
//
//	docker run --rm --network bangla-news-aggregator_default \
//	  -v bangla-news-aggregator_images:/app/data/images -v "$PWD":/src -w /src \
//	  -e DATABASE_URL=postgres://postgres:postgres@db:5432/bangla_news?sslmode=disable \
//	  -e IMAGE_STORAGE_DIR=/app/data/images golang:1.25-alpine \
//	  go run ./cmd/tools/cache_published_images.go
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/imagestore"
	"bangla-news-aggregator/internal/repository"
	"bangla-news-aggregator/internal/rss"
)

func main() {
	statusFlag := flag.String("status", "published", "comma-separated article statuses to process")
	idsFlag := flag.String("ids", "", "optional comma-separated article ids (overrides -status)")
	archiveFlag := flag.Bool("archive", true, "archive articles for which no usable photo is found")
	flag.Parse()
	cfg := config.Load()
	if cfg.ImageStorageDir == "" {
		log.Fatal("IMAGE_STORAGE_DIR is empty; nothing to cache into")
	}
	db, err := repository.OpenPostgresDB(cfg)
	if err != nil {
		log.Fatal(err)
	}
	store := imagestore.New(cfg.ImageStorageDir, "/img")
	scraper := rss.NewClient()

	var articles []domain.Article
	q := db.Select("id", "title", "source_url", "image_url")
	if *idsFlag != "" {
		q = q.Where("id IN ?", strings.Split(*idsFlag, ","))
	} else {
		q = q.Where("status IN ?", strings.Split(*statusFlag, ","))
	}
	if err := q.Find(&articles).Error; err != nil {
		log.Fatal(err)
	}
	log.Printf("checking %d articles", len(articles))

	var mu sync.Mutex
	var cached, kept, rescued, archived int
	jobs := make(chan domain.Article)
	var wg sync.WaitGroup
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := range jobs {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				result, local := resolve(ctx, store, scraper, a)
				cancel()

				mu.Lock()
				switch result {
				case "kept":
					kept++
				case "cached", "rescued":
					if err := db.Model(&domain.Article{}).Where("id = ?", a.ID).Update("image_url", local).Error; err != nil {
						log.Printf("update %s: %v", a.ID, err)
					}
					if result == "cached" {
						cached++
					} else {
						rescued++
					}
				default:
					if !*archiveFlag {
						log.Printf("no usable photo (left as is): %s", a.Title)
						break
					}
					if err := db.Model(&domain.Article{}).Where("id = ?", a.ID).Update("status", "archived").Error; err != nil {
						log.Printf("archive %s: %v", a.ID, err)
					}
					archived++
					log.Printf("archived (no usable photo): %s", a.Title)
				}
				mu.Unlock()
			}
		}()
	}
	for _, a := range articles {
		jobs <- a
	}
	close(jobs)
	wg.Wait()
	log.Printf("done: already local=%d, downloaded=%d, re-found from source page=%d, archived=%d", kept, cached, rescued, archived)
}

func resolve(ctx context.Context, store *imagestore.Store, scraper *rss.Client, a domain.Article) (string, string) {
	img := strings.TrimSpace(a.ImageURL)
	if strings.HasPrefix(img, "/img/") {
		if _, err := os.Stat(filepath.Join(store.Dir, strings.TrimPrefix(img, "/img/"))); err == nil {
			return "kept", img
		}
		img = "" // file lost: fall through to re-finding it on the source page
	}
	if img != "" {
		if local, err := store.Fetch(ctx, img); err == nil {
			return "cached", local
		}
	}
	if a.SourceURL != "" {
		if _, found, _ := scraper.Enrich(ctx, a.SourceURL); found != "" && found != img {
			if local, err := store.Fetch(ctx, found); err == nil {
				return "rescued", local
			}
		}
	}
	return "archive", ""
}
