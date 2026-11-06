package main

import (
	"fmt"
	"regexp"

	"github.com/mmcdole/gofeed"
)

var imgRegex = regexp.MustCompile(`<img[^>]+src=["']([^"']+)["']`)

func extractImage(item *gofeed.Item) string {
	if item.Image != nil && item.Image.URL != "" {
		return item.Image.URL
	}
	for _, enc := range item.Enclosures {
		if enc.URL != "" {
			return enc.URL
		}
	}
	if media, ok := item.Extensions["media"]; ok {
		if thumbs, ok := media["thumbnail"]; ok && len(thumbs) > 0 {
			if u := thumbs[0].Attrs["url"]; u != "" {
				return u
			}
		}
		if contents, ok := media["content"]; ok && len(contents) > 0 {
			if u := contents[0].Attrs["url"]; u != "" {
				return u
			}
		}
	}
	// Try parsing <img> from description / content
	if m := imgRegex.FindStringSubmatch(item.Description); len(m) > 1 {
		return m[1]
	}
	if m := imgRegex.FindStringSubmatch(item.Content); len(m) > 1 {
		return m[1]
	}
	return ""
}

func main() {
	fp := gofeed.NewParser()
	feeds := map[string]string{
		"BBC Bangla": "https://feeds.bbci.co.uk/bengali/rss.xml",
		"Channel i":  "https://www.channelionline.com/feed",
		"NTV Online": "https://www.ntvbd.com/rss.xml",
		"Jagonews24": "https://www.jagonews24.com/rss/rss.xml",
		"Prothom Alo": "https://www.prothomalo.com/feed",
	}

	for name, url := range feeds {
		f, err := fp.ParseURL(url)
		if err != nil {
			fmt.Printf("%s err: %v\n", name, err)
			continue
		}
		found := 0
		for _, it := range f.Items {
			img := extractImage(it)
			if img != "" {
				found++
			}
		}
		fmt.Printf("✅ %s: %d/%d items have IMAGES! Sample: %s\n", name, found, len(f.Items), extractImage(f.Items[0]))
	}
}
