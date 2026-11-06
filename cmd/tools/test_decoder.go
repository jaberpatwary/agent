package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func extractArticleID(rssURL string) (string, error) {
	u, err := url.Parse(rssURL)
	if err != nil {
		return "", err
	}
	segments := strings.Split(strings.TrimRight(u.Path, "/"), "/")
	if len(segments) > 0 {
		return segments[len(segments)-1], nil
	}
	return "", fmt.Errorf("invalid google news url path")
}

func fetchMetadata(client *http.Client, articleID string) (signature string, timestamp string, err error) {
	targetURL := fmt.Sprintf("https://news.google.com/articles/%s", articleID)

	req, _ := http.NewRequest("GET", targetURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	bodyStr := string(bodyBytes)

	sgRe := regexp.MustCompile(`data-n-a-sg="([^"]+)"`)
	tsRe := regexp.MustCompile(`data-n-a-ts="([^"]+)"`)

	sgMatch := sgRe.FindStringSubmatch(bodyStr)
	tsMatch := tsRe.FindStringSubmatch(bodyStr)

	if len(sgMatch) < 2 || len(tsMatch) < 2 {
		return "", "", fmt.Errorf("failed to extract Google News signature/timestamp metadata")
	}

	return sgMatch[1], tsMatch[1], nil
}

func DecodeGoogleNewsURL(client *http.Client, rssURL string) (string, error) {
	if !strings.Contains(rssURL, "news.google.com") {
		return rssURL, nil
	}
	articleID, err := extractArticleID(rssURL)
	if err != nil {
		return "", err
	}

	signature, timestamp, err := fetchMetadata(client, articleID)
	if err != nil {
		return "", err
	}

	innerReq, _ := json.Marshal([]any{
		"garturlreq",
		[]any{
			[]any{"X", "X", []any{"X", "X"}, nil, nil, 1, 1, "US:en", nil, 1, nil, nil, nil, nil, nil, 0, 1},
			"X",
			"X",
			1,
			[]any{1, 1, 1},
			1,
			1,
			nil,
			0,
			0,
			nil,
			0,
		},
		articleID,
		timestamp,
		signature,
	})

	rpcEnvelope, _ := json.Marshal([]any{
		[]any{
			"Fbv4je",
			string(innerReq),
			nil,
			"generic",
		},
	})

	formData := url.Values{}
	formData.Set("f.req", string(rpcEnvelope))

	req, err := http.NewRequest("POST", "https://news.google.com/_/DotsSplashUi/data/batchexecute?rpcids=Fbv4je", strings.NewReader(formData.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	rawResp := string(bodyBytes)
	rawResp = strings.TrimPrefix(rawResp, ")]}'\n\n")
	rawResp = strings.TrimPrefix(rawResp, ")]}'")

	var outerJSON []any
	if err := json.Unmarshal([]byte(rawResp), &outerJSON); err != nil {
		return "", fmt.Errorf("failed to parse outer json response: %v", err)
	}

	if len(outerJSON) > 0 {
		innerArr, ok := outerJSON[0].([]any)
		if ok && len(innerArr) > 2 {
			resString, ok := innerArr[2].(string)
			if ok {
				var innerResp []any
				if err := json.Unmarshal([]byte(resString), &innerResp); err == nil && len(innerResp) > 1 {
					if finalURL, ok := innerResp[1].(string); ok {
						return finalURL, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("could not parse target URL from batchexecute response: %s", rawResp)
}

func main() {
	client := &http.Client{Timeout: 10 * time.Second}
	rssLink := "https://news.google.com/rss/articles/CBMizwRBVV95cUxPN0lVbTBPcVlsUVZic19xbFZsNUlUMy1NNlBZTEZFbC0wcS1Ub2pNUjlocmZaYUdrQUNOYTZWdzZtM3MyZEtMWWFOUEZ3YzVsdURGR1JvM2NycnNvLTFmaTV3NXVRTzZIT0dnazhlb2dZV1prTG5DQVpVODRveWx4OWV3QVdyZHFXckNLN2FJdnhiMGVlM3Y0WW9fTVFsdTVnUGhEcHFsZEw2M2JQYkJjUkdtY2NCTHUzY0JCaFJCbk4teDdHWGhLV0VDaWJMbG5zS2FiTjliNkZoQ0Q5eHhtZmZZakhoZjlxLVdtTmk0bzFRVVFFSGl4T2p6bWdwWHVtTEU5cW1pNTlVVkJLREQ5T1RCWG9vYXdQQjJOemRBUkFlUUlRZVRpZzBLdUduYVBMQkZnZ0tYYlBJSTJ3NldjOERfanNxaVRTQnl0QlRTeVc5SGJtSzUzSFBnek9KYmVCZXYyUGw1cDZSYXBGeEdNRE5kLU1SSEp4azNjenlaNXlrMW5BOWRobEJiTk92c21rNHQzdmNqNE8xRTh3dExjeXpGLURicjlkZ1g3VWExV3h4eXRMS3BoMFJrdkhieXJkbmFLTGJmSUtpNEtOSVBpOWtRSEJLQXFtZWJ6ejJubnB3WWhsX292djZJXzltd1UxVkZVVm5LQ3pGOXA5NGpSOEozUEQ3LXpUX3VkVEVSSFFQUEZ2dGFvV2RXNkZpUzFfWE9vN1dvV09WS0JocGsyTGc5OHBaQWFvc1hWWG9jOHVIdWRyUVNjOVBaenlMV2s?oc=5"

	cleanURL, err := DecodeGoogleNewsURL(client, rssLink)
	if err != nil {
		fmt.Printf("Error decoding URL: %v\n", err)
		return
	}

	fmt.Printf("🎉 SUCCESS! Decoded URL: %s\n", cleanURL)
}
