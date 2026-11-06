package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mmcdole/gofeed"
)

func testFeed(name, url string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("❌ %-25s HTTP Error: %v\n", name, err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Printf("❌ %-25s Status: %d\n", name, resp.StatusCode)
		return false
	}

	fp := gofeed.NewParser()
	feed, err := fp.Parse(resp.Body)
	if err != nil {
		fmt.Printf("❌ %-25s XML Parse error: %v\n", name, err)
		return false
	}

	if len(feed.Items) == 0 {
		fmt.Printf("⚠️ %-25s 0 items\n", name)
		return false
	}

	fmt.Printf("✅ %-25s Items: %3d | First: %s | URL: %s\n", name, len(feed.Items), feed.Items[0].Title, url)
	return true
}

func main() {
	candidates := map[string][]string{
		"Prothom Alo":        {"https://www.prothomalo.com/feed"},
		"BBC Bangla":         {"https://feeds.bbci.co.uk/bengali/rss.xml"},
		"NTV Online":         {"https://www.ntvbd.com/rss.xml"},
		"Channel i":          {"https://www.channelionline.com/feed"},
		"Jagonews24":         {"https://www.jagonews24.com/rss/rss.xml"},
		"DW Bangla":          {"https://rss.dw.com/xml/rss-ben-all"},
		"Techshohor":         {"https://techshohor.com/feed"},
		"Dhaka Post":         {"https://www.dhakapost.com/rss", "https://www.dhakapost.com/feed"},
		"Dhaka Tribune":      {"https://bangla.dhakatribune.com/rss.xml", "https://bangla.dhakatribune.com/feed"},
		"Bangla Tribune":     {"https://www.banglatribune.com/rss.xml", "https://www.banglatribune.com/feed"},
		"BDNews24 Bangla":    {"https://bangla.bdnews24.com/rss", "https://bangla.bdnews24.com/rss.xml", "https://bangla.bdnews24.com/feed"},
		"RisingBD":           {"https://www.risingbd.com/rss", "https://www.risingbd.com/rss.xml", "https://www.risingbd.com/feed"},
		"Barta24":            {"https://barta24.com/rss.xml", "https://barta24.com/feed"},
		"Daily Bangladesh":   {"https://www.daily-bangladesh.com/rss/rss.xml", "https://www.daily-bangladesh.com/feed"},
		"Somoy News":         {"https://somoynews.tv/rss.xml", "https://www.somoynews.tv/feed", "https://somoynews.tv/feed/rss.xml"},
		"Kalbela":            {"https://www.kalbela.com/rss.xml", "https://www.kalbela.com/feed"},
		"Jugantor":           {"https://www.jugantor.com/feed", "https://www.jugantor.com/rss.xml"},
		"Samakal":            {"https://samakal.com/rss.xml", "https://samakal.com/feed"},
		"Ittefaq":            {"https://www.ittefaq.com.bd/rss.xml", "https://www.ittefaq.com.bd/feed"},
		"Manabzamin":         {"https://mzamin.com/rss.xml", "https://mzamin.com/feed"},
		"Inqilab":            {"https://dailyinqilab.com/rss.xml", "https://dailyinqilab.com/feed"},
		"Janakantha":         {"https://www.dailyjanakantha.com/rss.xml", "https://www.dailyjanakantha.com/feed"},
		"Kaler Kantho":       {"https://www.kalerkantho.com/rss.xml", "https://www.kalerkantho.com/feed"},
		"Bhorer Kagoj":       {"https://www.bhorerkagoj.com/feed", "https://www.bhorerkagoj.com/rss.xml"},
		"Desh Rupantor":      {"https://www.deshrupantor.com/rss.xml", "https://www.deshrupantor.com/feed"},
		"Jaijaidin":          {"https://www.jaijaidinbd.com/rss.xml", "https://www.jaijaidinbd.com/feed"},
		"Amader Shomoy":      {"https://dainikamadershomoy.com/rss.xml", "https://dainikamadershomoy.com/feed"},
		"ShareBiz":           {"https://sharebiz.net/feed"},
		"Bonik Barta":        {"https://bonikbarta.net/feed", "https://bonikbarta.net/rss.xml"},
		"DigiExpress":        {"https://digiexpress.net/feed"},
	}

	for name, urls := range candidates {
		for _, u := range urls {
			if testFeed(name, u) {
				break
			}
		}
	}
}
