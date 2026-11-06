package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	url := "https://samakal.com/bangladesh/article/379991/ডেঙ্গুতে-এক-দিনে-রেকর্ড-১১-জনের-মৃত্যু"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(string(body)))

	// Let's find where the article text is:
	doc.Find("p").Each(func(i int, s *goquery.Selection) {
		parentClass, _ := s.Parent().Attr("class")
		parentTag := goquery.NodeName(s.Parent())
		text := strings.TrimSpace(s.Text())
		if len([]rune(text)) > 20 {
			fmt.Printf("P tag [%s.%s]: %s\n\n", parentTag, parentClass, text)
		}
	})
}
