package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

func inspectFeed(url string) {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	xml := string(body)

	// find <item>
	itemStart := strings.Index(xml, "<item>")
	if itemStart != -1 {
		itemEnd := strings.Index(xml[itemStart:], "</item>")
		if itemEnd != -1 {
			fmt.Printf("\n=== %s ===\n%s\n", url, xml[itemStart:itemStart+itemEnd+7])
		}
	}
}

func main() {
	inspectFeed("https://techshohor.com/feed")
	inspectFeed("https://www.channelionline.com/feed")
}
