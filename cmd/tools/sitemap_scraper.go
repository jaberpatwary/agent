package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"bangla-news-aggregator/internal/ai"
	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

// XMLSitemap for standard sitemaps
type XMLSitemap struct {
	XMLName xml.Name      `xml:"urlset"`
	URLs    []XMLSitemapURL `xml:"url"`
}

type XMLSitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

// XMLSitemapIndex for sitemap indexes
type XMLSitemapIndex struct {
	XMLName  xml.Name          `xml:"sitemapindex"`
	Sitemaps []XMLSitemapEntry `xml:"sitemap"`
}

type XMLSitemapEntry struct {
	Loc string `xml:"loc"`
}

var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

type SitemapSource struct {
	Name        string
	Category    string
	SitemapURLs []string   // try these sitemap URLs in order
	URLFilter   string     // only include URLs matching this substring
	MaxArticles int        // max articles to fetch per run
}

func fetchURL(client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
}

func parseSitemap(data []byte) []string {
	var urls []string

	// Try sitemap index first
	var index XMLSitemapIndex
	if xml.Unmarshal(data, &index) == nil && len(index.Sitemaps) > 0 {
		for _, s := range index.Sitemaps {
			if s.Loc != "" {
				urls = append(urls, s.Loc)
			}
		}
		return urls
	}

	// Try standard urlset
	var sitemap XMLSitemap
	if xml.Unmarshal(data, &sitemap) == nil && len(sitemap.URLs) > 0 {
		for _, u := range sitemap.URLs {
			if u.Loc != "" {
				urls = append(urls, u.Loc)
			}
		}
		return urls
	}

	return nil
}

func scrapeArticle(client *http.Client, pageURL string) (title, content, imgURL string) {
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "bn-BD,bn;q=0.9,en-US;q=0.8,en;q=0.7")

	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return
	}

	// Title
	title = strings.TrimSpace(doc.Find(`meta[property="og:title"]`).AttrOr("content", ""))
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(doc.Find("title").Text())
	}

	// Image
	imgURL = doc.Find(`meta[property="og:image"]`).AttrOr("content", "")
	if imgURL == "" {
		imgURL = doc.Find(`meta[name="twitter:image"]`).AttrOr("content", "")
	}
	if imgURL == "" {
		imgURL = doc.Find(`meta[itemprop="image"]`).AttrOr("content", "")
	}
	// Resolve relative image URL
	if imgURL != "" && strings.HasPrefix(imgURL, "/") && !strings.HasPrefix(imgURL, "//") {
		parts := strings.Split(pageURL, "/")
		if len(parts) >= 3 {
			imgURL = parts[0] + "//" + parts[2] + imgURL
		}
	}

	// Content
	seenParas := make(map[string]bool)
	var paras []string
	selectors := ".details-body p, .details-main p, .dNewsDesc p, .entry-content p, .post-content p, .detail-box p, .details-text p, .content-details p, .news-details p, .jw-detail p, [itemprop='articleBody'] p, .story-body p, .article-body p, .story-element p, article p, main p"
	doc.Find(selectors).Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if utf8.RuneCountInString(t) > 25 && !seenParas[t] {
			if strings.HasPrefix(t, "ছবির উৎস") || strings.HasPrefix(t, "ছবি ক্যাপশন") ||
				strings.Contains(t, "ALL RIGHTS RESERVED") || strings.Contains(t, "বিজ্ঞাপন") {
				return
			}
			seenParas[t] = true
			paras = append(paras, "<p>"+t+"</p>")
		}
	})
	content = strings.Join(paras, "\n")
	return
}

func main() {
	sources := []SitemapSource{
		{
			Name:        "দৈনিক সমকাল",
			Category:    "general",
			SitemapURLs: []string{"https://samakal.com/sitemap.xml", "https://www.samakal.com/sitemap.xml"},
			URLFilter:   "samakal.com/",
			MaxArticles: 20,
		},
		{
			Name:        "দৈনিক যুগান্তর",
			Category:    "general",
			SitemapURLs: []string{"https://www.jugantor.com/sitemap.xml", "https://jugantor.com/sitemap.xml"},
			URLFilter:   "jugantor.com/",
			MaxArticles: 20,
		},
		{
			Name:        "দৈনিক কালবেলা",
			Category:    "general",
			SitemapURLs: []string{"https://www.kalbela.com/sitemap.xml", "https://kalbela.com/sitemap.xml"},
			URLFilter:   "kalbela.com/",
			MaxArticles: 20,
		},
		{
			Name:        "দৈনিক মানবজমিন",
			Category:    "general",
			SitemapURLs: []string{"https://mzamin.com/sitemap.xml", "https://www.mzamin.com/sitemap.xml"},
			URLFilter:   "mzamin.com/",
			MaxArticles: 20,
		},
		{
			Name:        "দৈনিক কালের কণ্ঠ",
			Category:    "general",
			SitemapURLs: []string{"https://www.kalerkantho.com/sitemap.xml", "https://kalerkantho.com/sitemap.xml"},
			URLFilter:   "kalerkantho.com/",
			MaxArticles: 20,
		},
		{
			Name:        "বাংলা ট্রিবিউন",
			Category:    "general",
			SitemapURLs: []string{"https://www.banglatribune.com/sitemap.xml", "https://banglatribune.com/sitemap.xml"},
			URLFilter:   "banglatribune.com/",
			MaxArticles: 20,
		},
		{
			Name:        "ঢাকা পোস্ট",
			Category:    "general",
			SitemapURLs: []string{"https://www.dhakapost.com/sitemap.xml"},
			URLFilter:   "dhakapost.com/",
			MaxArticles: 20,
		},
		{
			Name:        "বার্তা২৪",
			Category:    "general",
			SitemapURLs: []string{"https://barta24.com/sitemap.xml", "https://www.barta24.com/sitemap.xml"},
			URLFilter:   "barta24.com/bangla",
			MaxArticles: 20,
		},
		{
			Name:        "দেশ রূপান্তর",
			Category:    "general",
			SitemapURLs: []string{"https://www.deshrupantor.com/sitemap.xml"},
			URLFilter:   "deshrupantor.com/",
			MaxArticles: 20,
		},
		{
			Name:        "বিডিনিউজ২৪ বাংলা",
			Category:    "general",
			SitemapURLs: []string{"https://bangla.bdnews24.com/sitemap.xml"},
			URLFilter:   "bangla.bdnews24.com/",
			MaxArticles: 20,
		},
	}

	cfg := config.Load()
	db, err := repository.NewPostgresDB(cfg)
	if err != nil {
		log.Fatalf("DB error: %v", err)
	}
	articleRepo := repository.NewArticleRepository(db)
	aiClient := ai.NewClient(cfg)

	httpClient := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	totalSaved := 0

	for _, src := range sources {
		fmt.Printf("\n📰 Processing: %s\n", src.Name)

		// Find working sitemap URL
		var articleURLs []string
		for _, smURL := range src.SitemapURLs {
			data, err := fetchURL(httpClient, smURL)
			if err != nil {
				fmt.Printf("  ❌ sitemap %s: %v\n", smURL, err)
				continue
			}

			parsed := parseSitemap(data)
			if len(parsed) == 0 {
				fmt.Printf("  ⚠️  sitemap %s: no URLs parsed\n", smURL)
				continue
			}

			// If this is a sitemap index, fetch the first child sitemap
			isIndex := false
			for _, p := range parsed {
				if strings.Contains(p, "sitemap") || strings.HasSuffix(p, ".xml") {
					isIndex = true
					break
				}
			}

			if isIndex {
				fmt.Printf("  📂 Sitemap index with %d children, fetching last (most recent)...\n", len(parsed))
				// Try from the end (most recent) until we find articles matching our filter
				for i := len(parsed) - 1; i >= 0 && len(articleURLs) == 0; i-- {
					// Skip child sitemaps that clearly don't match our filter
					if src.URLFilter != "" && !strings.Contains(parsed[i], strings.Split(src.URLFilter, "/")[0]) {
						// still try it — filter is on article URLs not sitemap URLs
					}
					childData, err := fetchURL(httpClient, parsed[i])
					if err != nil {
						continue
					}
					childURLs := parseSitemap(childData)
					for _, u := range childURLs {
						if src.URLFilter == "" || strings.Contains(u, src.URLFilter) {
							articleURLs = append(articleURLs, u)
						}
					}
					if len(articleURLs) > 0 {
						fmt.Printf("  ✅ Got %d matching URLs from child sitemap: %s\n", len(articleURLs), parsed[i])
					} else {
						fmt.Printf("  ⏭️  Skipping non-matching sitemap: %s\n", parsed[i])
					}
				}
			} else {
				for _, u := range parsed {
					if src.URLFilter == "" || strings.Contains(u, src.URLFilter) {
						articleURLs = append(articleURLs, u)
					}
				}
				fmt.Printf("  ✅ sitemap %s: %d URLs\n", smURL, len(articleURLs))
			}

			if len(articleURLs) > 0 {
				break
			}
		}

		if len(articleURLs) == 0 {
			fmt.Printf("  ⛔ No article URLs found for %s\n", src.Name)
			continue
		}

		// Limit and take the most recent (last ones)
		if len(articleURLs) > src.MaxArticles {
			articleURLs = articleURLs[len(articleURLs)-src.MaxArticles:]
		}

		fmt.Printf("  📥 Will process %d articles\n", len(articleURLs))

		// Process articles in parallel
		var mu sync.Mutex
		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		saved := 0

		for _, articleURL := range articleURLs {
			wg.Add(1)
			sem <- struct{}{}
			go func(url string) {
				defer wg.Done()
				defer func() { <-sem }()

				// Check if already exists
				exists, _ := articleRepo.ExistsBySourceURL(url)
				if exists {
					return
				}

				title, content, imgURL := scrapeArticle(httpClient, url)
				if title == "" || len(content) < 200 {
					return
				}

				result, attempts := aiClient.Summarize(context.Background(), title, content)
				_ = attempts

				summary := result.Text
				if result.Err != nil || summary == "" {
					cleaned := htmlTagRegex.ReplaceAllString(content, " ")
					cleaned = strings.Join(strings.Fields(cleaned), " ")
					if utf8.RuneCountInString(cleaned) > 280 {
						runes := []rune(cleaned)
						cleaned = string(runes[:280]) + "..."
					}
					summary = cleaned
				}

				article := &domain.Article{
					Title:       title,
					Content:     content,
					Summary:     summary,
					SourceURL:   url,
					SourceName:  src.Name,
					ImageURL:    imgURL,
					Category:    src.Category,
					Status:      "published",
					PublishedAt: time.Now(),
				}

				if err := articleRepo.Create(article); err != nil {
					return
				}

				mu.Lock()
				saved++
				fmt.Printf("    ✅ [%s] %s\n", src.Name, title)
				mu.Unlock()
			}(articleURL)
		}
		wg.Wait()
		totalSaved += saved
		fmt.Printf("  💾 Saved %d new articles from %s\n", saved, src.Name)
	}

	fmt.Printf("\n🎉 Total new articles saved: %d\n", totalSaved)
}
