package imagestore

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// photo paints a noisy "photo" and, when band > 0, a flat navy strip of that
// many rows across the bottom with some lighter "slogan" boxes inside it.
func photo(w, h, band int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8((x*7 + y*13) % 200)
			img.Set(x, y, color.RGBA{v, uint8(255 - int(v)), uint8(v / 2), 255})
		}
	}
	for y := h - band; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{18, 42, 120, 255}
			if x > 40 && x < w/2 && y > h-band+15 && y < h-15 {
				c = color.RGBA{230, 200, 40, 255} // the advertiser's slogan
			}
			img.Set(x, y, c)
		}
	}
	return img
}

func TestSponsorBandIsFound(t *testing.T) {
	if top := sponsorBandTop(photo(725, 407, 90)); top != 316 {
		t.Fatalf("band top = %d, want 316 (one row above the band)", top)
	}
}

func TestPhotosWithoutABandAreLeftAlone(t *testing.T) {
	if top := sponsorBandTop(photo(725, 407, 0)); top != -1 {
		t.Fatalf("plain photo flagged at row %d", top)
	}
	// A flat strip that is too thin (a frame line) or too tall (a sky) is
	// not a banner.
	if top := sponsorBandTop(photo(725, 407, 20)); top != -1 {
		t.Fatalf("thin strip flagged at row %d", top)
	}
	if top := sponsorBandTop(photo(725, 407, 200)); top != -1 {
		t.Fatalf("tall flat area flagged at row %d", top)
	}
}

func TestTrimSponsorBandRewritesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.jpg")
	f, _ := os.Create(path)
	if err := jpeg.Encode(f, photo(725, 407, 90), &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	f.Close()
	ok, err := TrimSponsorBand(path)
	if err != nil || !ok {
		t.Fatalf("trim = %v, %v", ok, err)
	}
	f, _ = os.Open(path)
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 725 || cfg.Height < 314 || cfg.Height > 320 {
		t.Fatalf("trimmed to %dx%d", cfg.Width, cfg.Height)
	}
	if ok, _ := TrimSponsorBand(path); ok {
		t.Fatal("a trimmed photo was trimmed again")
	}
}

// SPONSOR_SCAN_DIR=<dir of cached images> SPONSOR_SCAN_MAP=<file of "name|source"
// lines> reports, per source, how many cached photos carry a band.
func TestScanCachedImages(t *testing.T) {
	dir := os.Getenv("SPONSOR_SCAN_DIR")
	if dir == "" {
		t.Skip("set SPONSOR_SCAN_DIR to scan a directory of cached images")
	}
	source := map[string]string{}
	if m, err := os.ReadFile(os.Getenv("SPONSOR_SCAN_MAP")); err == nil {
		for _, line := range strings.Split(string(m), "\n") {
			if name, src, ok := strings.Cut(strings.TrimSpace(line), "|"); ok {
				source[name] = src
			}
		}
	}
	hit, all := map[string]int{}, map[string]int{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		img, _, err := image.Decode(f)
		f.Close()
		src := source[e.Name()]
		all[src]++
		if err != nil {
			continue
		}
		if top := sponsorBandTop(img); top >= 0 {
			hit[src]++
			t.Logf("band %s %s %dx%d top=%d", src, e.Name(), img.Bounds().Dx(), img.Bounds().Dy(), top)
		} else if src != "" && src == os.Getenv("SPONSOR_SCAN_MISSES") {
			t.Logf("miss %s %s %dx%d", src, e.Name(), img.Bounds().Dx(), img.Bounds().Dy())
		}
	}
	for src, n := range all {
		t.Logf("%-16s %d of %d", src, hit[src], n)
	}
}
