package usecase

import "testing"

func TestValidateFeedURL(t *testing.T) {
	tests := []struct {
		name, input string
		wantErr     bool
	}{
		{"https accepted", "https://example.com/rss.xml", false},
		{"http accepted", "http://example.com/feed", false},
		{"reject ftp", "ftp://example.com/feed", true},
		{"reject localhost", "http://localhost:8080/feed", true},
		{"reject loopback IP", "http://127.0.0.1/feed", true},
		{"reject private IP", "http://192.168.1.10/feed", true},
		{"reject malformed", "not a URL", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateFeedURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateFeedURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got == "" {
				t.Fatal("expected canonical URL")
			}
		})
	}
}
