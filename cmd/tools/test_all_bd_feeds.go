package main

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/mmcdole/gofeed"
)

var imgRegex = regexp.MustCompile(`<img[^>]+src=["']([^"']+)["']`)

func testFeed(name, url string) {
	fp := gofeed.NewParser()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return
	}

	feed, err := fp.Parse(resp.Body)
	if err != nil || len(feed.Items) == 0 {
		return
	}

	item := feed.Items[0]
	img := ""
	if item.Image != nil && item.Image.URL != "" {
		img = item.Image.URL
	} else if len(item.Enclosures) > 0 {
		img = item.Enclosures[0].URL
	} else if media, ok := item.Extensions["media"]; ok {
		if t, ok := media["thumbnail"]; ok && len(t) > 0 {
			img = t[0].Attrs["url"]
		} else if c, ok := media["content"]; ok && len(c) > 0 {
			img = c[0].Attrs["url"]
		}
	} else if m := imgRegex.FindStringSubmatch(item.Description); len(m) > 1 {
		img = m[1]
	} else if m := imgRegex.FindStringSubmatch(item.Content); len(m) > 1 {
		img = m[1]
	}

	contentLen := len(item.Content)
	if len(item.Description) > contentLen {
		contentLen = len(item.Description)
	}

	hasPic := "❌ No Pic"
	if img != "" {
		hasPic = "📸 PIC OK"
	}
	hasContent := "❌ Short"
	if contentLen > 250 {
		hasContent = "📝 FULL NEWS"
	}

	fmt.Printf("[%s] [%s] %s (%d items, len: %d) | %s\n", hasPic, hasContent, name, len(feed.Items), contentLen, url)
}

func main() {
	urls := map[string]string{
		"Prothom Alo (প্রথম আলো)":            "https://www.prothomalo.com/feed",
		"BBC Bangla (বিবিসি বাংলা)":           "https://feeds.bbci.co.uk/bengali/rss.xml",
		"NTV Online (এনটিভি)":                "https://www.ntvbd.com/rss.xml",
		"Channel i (চ্যানেল আই)":             "https://www.channelionline.com/feed",
		"DW Bangla (ডয়েচে ভেলে)":             "https://rss.dw.com/xml/rss-ben-all",
		"Jagonews24 (জাগো নিউজ)":             "https://www.jagonews24.com/rss/rss.xml",
		"Dhaka Post":                         "https://www.dhakapost.com/rss",
		"Dhaka Tribune Bangla":               "https://bangla.dhakatribune.com/rss.xml",
		"Daily Bangladesh":                   "https://www.daily-bangladesh.com/rss/rss.xml",
		"Barta24":                            "https://barta24.com/rss.xml",
		"Bhorer Kagoj":                       "https://www.bhorerkagoj.com/feed",
		"Desh Rupantor":                      "https://www.deshrupantor.com/rss.xml",
		"Somoyer Alo":                        "https://www.shomoyeralo.com/rss.xml",
		"Amader Shomoy":                      "https://dainikamadershomoy.com/rss.xml",
		"ShareBiz":                           "https://sharebiz.net/feed",
		"Techshohor (টেকশহর)":                "https://techshohor.com/feed",
		"DigiExpress":                        "https://digiexpress.net/feed",
	}

	for name, url := range urls {
		testFeed(name, url)
	}
}
