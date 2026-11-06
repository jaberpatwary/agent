package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func testURL(name, url string) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("❌ %s err: %v\n", name, err)
		return
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		fmt.Printf("❌ %s parse err: %v\n", name, err)
		return
	}

	seenParas := make(map[string]bool)
	var paras []string
	selectors := ".details-body p, .details-main p, .dNewsDesc p, .entry-content p, .post-content p, .detail-box p, .details-text p, .content-details p, .news-details p, .jw-detail p, [itemprop='articleBody'] p, .story-body p, .article-body p, .story-element p, article p, main p, [role='main'] p"
	doc.Find(selectors).Each(func(i int, s *goquery.Selection) {
		t := strings.TrimSpace(s.Text())
		if len([]rune(t)) > 25 && !seenParas[t] {
			if strings.HasPrefix(t, "ছবির উৎস") || strings.HasPrefix(t, "ছবি ক্যাপশন") || strings.Contains(t, "ALL RIGHTS RESERVED") || strings.Contains(t, "বিজ্ঞাপন :") || strings.Contains(t, "Blazing-fast frontend") {
				return
			}
			seenParas[t] = true
			paras = append(paras, "<p>"+t+"</p>")
		}
	})

	totalLen := len(strings.Join(paras, "\n"))
	fmt.Printf("✅ %-18s: %2d paras | %5d chars | %s\n", name, len(paras), totalLen, url)
	if len(paras) > 0 {
		fmt.Printf("   1st para: %s\n", paras[0])
	}
}

func main() {
	testURL("Daily Bangladesh", "https://daily-bangladesh.com/international/539643")
	testURL("BBC Bangla", "https://www.bbc.com/bengali/articles/c6pwg0v1wg80o?at_medium=RSS&at_campaign=rss")
	testURL("RisingBD", "https://www.risingbd.com/bangladesh/news/668270")
	testURL("Channel i", "https://www.channelionline.com/bombay-hc-keeps-frros-leave-india-notice-to-actor-imran-khan/")
	testURL("Techshohor", "https://techshohor.com/201254/")
	testURL("Prothom Alo", "https://www.prothomalo.com/bangladesh/district/gfxycx3vkw")
}
