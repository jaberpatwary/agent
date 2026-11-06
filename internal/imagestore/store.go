// Package imagestore downloads a publisher's hero image once and serves it
// back from this portal's own domain instead of hotlinking the source
// forever. Hotlinking breaks the moment a source blocks cross-site requests,
// changes its CDN, or deletes the image — this package turns that into a
// one-time fetch that then keeps working regardless of what the source does.
package imagestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store downloads and caches images on local disk under Dir, keyed by a
// hash of the source URL so re-processing the same URL is a cheap no-op.
//
// Local disk is a pragmatic choice for a single-instance deployment; it is
// NOT durable across a platform that wipes disk on redeploy (check your
// host's docs — e.g. Render's free web service disk is ephemeral). If that
// matters for your deployment, mount a persistent volume at Dir, or swap
// Save's os.WriteFile call for an object-storage upload (S3/R2/GCS) — the
// rest of the pipeline only depends on Save returning a path this server can
// serve back under PublicPrefix.
type Store struct {
	Dir          string // filesystem directory to save files in
	PublicPrefix string // URL path prefix this server serves Dir under, e.g. "/img"
	MaxBytes     int64
	MinBytes     int64 // smaller files are rejected as icons, logos or tracking pixels rather than real photos
	MinWidth     int   // narrower images are rejected as thumbnails (checked for JPEG/PNG/GIF; other formats pass)
	http         *http.Client
}

func New(dir, publicPrefix string) *Store {
	return &Store{
		Dir:          dir,
		PublicPrefix: strings.TrimRight(publicPrefix, "/"),
		MaxBytes:     8 << 20, // 8 MB is generous for a news hero image and caps abuse
		MinBytes:     8 << 10, // a real news photo is comfortably above 8 KB
		MinWidth:     500,     // below this a hero/card image looks blurry
		http:         &http.Client{Timeout: 15 * time.Second},
	}
}

// Fetch downloads sourceURL (if not already cached) and returns the local
// public path (e.g. "/img/ab12cd34.jpg") to store on the article instead of
// the original remote URL. On any failure it returns ("", err) — callers
// should keep using the original remote URL in that case rather than losing
// the image entirely.
func (s *Store) Fetch(ctx context.Context, sourceURL string) (string, error) {
	if s == nil || s.Dir == "" || sourceURL == "" {
		return "", fmt.Errorf("imagestore: not configured")
	}
	// sourceURL comes from RSS feed content (og:image, <enclosure>, <img> src)
	// — not something an admin typed in, unlike a feed's own URL. A
	// malicious or compromised publisher could embed a URL pointing at an
	// internal service or the cloud metadata endpoint, and without this
	// check this server-side fetch-and-cache step would dutifully request
	// it. Same guard as feed URL validation (see usecase.validateFeedURL).
	if err := rejectUnsafeURL(sourceURL); err != nil {
		return "", fmt.Errorf("imagestore: refusing to fetch: %w", err)
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", fmt.Errorf("imagestore: create dir: %w", err)
	}

	ext := extensionFor(sourceURL)
	name := hashName(sourceURL) + ext
	fullPath := filepath.Join(s.Dir, name)
	publicPath := s.PublicPrefix + "/" + name

	if info, err := os.Stat(fullPath); err == nil && info.Size() > 0 {
		return publicPath, nil // already cached from a previous run
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", fmt.Errorf("imagestore: build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BanglaNewsImageFetcher/1.0)")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("imagestore: fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("imagestore: fetch: http %d", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" && !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("imagestore: not an image (content-type %q)", contentType)
	}

	tmpPath := fullPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("imagestore: create file: %w", err)
	}
	written, err := io.Copy(out, io.LimitReader(resp.Body, s.MaxBytes+1))
	closeErr := out.Close()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("imagestore: write file: %w", err)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("imagestore: close file: %w", closeErr)
	}
	if written > s.MaxBytes {
		os.Remove(tmpPath)
		return "", fmt.Errorf("imagestore: image exceeds %d byte limit", s.MaxBytes)
	}
	if written < s.MinBytes {
		os.Remove(tmpPath)
		return "", fmt.Errorf("imagestore: image too small (%d bytes) to be a news photo", written)
	}
	if s.MinWidth > 0 {
		if w, ok := imageWidth(tmpPath); ok && w < s.MinWidth {
			os.Remove(tmpPath)
			return "", fmt.Errorf("imagestore: image only %dpx wide (need %dpx) — a thumbnail, not a news photo", w, s.MinWidth)
		}
	}
	if err := os.Rename(tmpPath, fullPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("imagestore: finalize file: %w", err)
	}

	return publicPath, nil
}

// rejectUnsafeURL blocks obvious SSRF targets: non-http(s) schemes, localhost,
// the cloud metadata hostname, and private/loopback/link-local IPs — whether
// given literally or resolved via DNS. It does not protect against DNS
// rebinding (a name that resolves to a public IP at check time but a
// private one at request time); a deployment that needs that guarantee
// should also apply network-level egress restrictions.
func rejectUnsafeURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Hostname() == "" {
		return fmt.Errorf("invalid or non-http(s) URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal" {
		return fmt.Errorf("local or metadata hostname not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isUnsafeIP(ip) {
			return fmt.Errorf("private or local IP address not allowed")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("could not resolve host: %w", err)
	}
	for _, ip := range ips {
		if isUnsafeIP(ip) {
			return fmt.Errorf("host resolves to a private or local IP address")
		}
	}
	return nil
}

func isUnsafeIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func hashName(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])[:24]
}

func extensionFor(url string) string {
	clean := strings.SplitN(url, "?", 2)[0]
	ext := strings.ToLower(filepath.Ext(clean))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return ext
	default:
		return ".jpg" // safe default; browsers sniff content anyway
	}
}

// imageWidth reads just the header of a JPEG/PNG/GIF file. ok is false for
// formats the standard library can't decode (e.g. WebP), which are allowed.
func imageWidth(path string) (int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, false
	}
	return cfg.Width, true
}

// CheckURL reports whether a URL is safe for this server to fetch (http(s),
// not localhost, not a private or metadata address). Shared with the agent's
// read_page tool.
func CheckURL(raw string) error { return rejectUnsafeURL(raw) }
