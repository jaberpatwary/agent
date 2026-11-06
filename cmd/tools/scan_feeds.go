package main

import (
	"context"
	"fmt"
	"time"

	"github.com/mmcdole/gofeed"
)

func test(name, url string) {
	fp := gofeed.NewParser()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	feed, err := fp.ParseURLWithContext(url, ctx)
	if err != nil {
		fmt.Printf("❌ %s (%s): %v\n", name, url, err)
		return
	}
	if len(feed.Items) == 0 {
		fmt.Printf("⚠️ %s: 0 items\n", name)
		return
	}

	item := feed.Items[0]
	img := ""
	if item.Image != nil {
		img = item.Image.URL
	} else if len(item.Enclosures) > 0 {
		img = item.Enclosures[0].URL
	} else if media, ok := item.Extensions["media"]; ok {
		if thumbs, ok := media["thumbnail"]; ok && len(thumbs) > 0 {
			img = thumbs[0].Attrs["url"]
		} else if contents, ok := media["content"]; ok && len(contents) > 0 {
			img = contents[0].Attrs["url"]
		}
	}

	fmt.Printf("✅ %s: %d items | ContentLen: %d | Img: %s\n", name, len(feed.Items), len(item.Content)+len(item.Description), img)
}

func main() {
	test("Prothom Alo All", "https://www.prothomalo.com/feed")
	test("Prothom Alo Bangladesh", "https://www.prothomalo.com/feed/category/bangladesh")
	test("Prothom Alo Sports", "https://www.prothomalo.com/feed/category/sports")
	test("Prothom Alo Business", "https://www.prothomalo.com/feed/category/business")
	test("Prothom Alo Tech", "https://www.prothomalo.com/feed/category/technology")
	test("Prothom Alo Ent", "https://www.prothomalo.com/feed/category/entertainment")

	test("BBC Bangla", "https://feeds.bbci.co.uk/bengali/rss.xml")
	test("DW Bangla (ডয়চে ভেলে)", "https://rss.dw.com/xml/rss-ben-all")
	test("VOA Bangla", "https://www.voabangla.com/api/zy$oq$emye")

	test("NTV Online", "https://www.ntvbd.com/rss.xml")
	test("Jagonews24", "https://www.jagonews24.com/rss/rss.xml")
	test("Dhaka Tribune Bangla", "https://bangla.dhakatribune.com/feed")
	test("Bangla Tribune", "https://www.banglatribune.com/feed")
	test("Channel i Online", "https://www.channelionline.com/feed")
	test("Somoy News", "https://somoynews.tv/feed")
	test("Kaler Kantho", "https://www.kalerkantho.com/feed/rss.xml")
	test("Daily Janakantha", "https://www.dailyjanakantha.com/rss/rss.xml")
	test("Bonik Barta", "https://bonikbarta.net/feed/rss.xml")
	test("Bhorer Kagoj", "https://www.bhorerkagoj.com/feed")
	test("Desh Rupantor", "https://www.deshrupantor.com/feed")
}
