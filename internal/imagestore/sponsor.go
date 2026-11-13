package imagestore

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

// Some outlets (RisingBD, for one) stamp an advertiser's banner across the
// bottom of every share image: a flat, strongly coloured strip with the
// sponsor's slogan and logo. Readers here should see the news photo, not
// someone else's advert, so the strip is cut off before the image is served.
//
// The strip is recognised by its shape, not by the advertiser: a band of one
// flat, saturated colour that reaches both side edges, starts at the same
// height on both sides, and covers 10–32% of the bottom of the picture.
// Ordinary photos rarely have flat colour on both bottom corners up to the
// same line, and when one does nothing is lost but a strip of that colour.

const (
	bandMinShare = 0.10
	bandMaxShare = 0.32
	bandTol      = 28 // how far a pixel may drift from the band colour, per channel
	bandMinSat   = 60 // max-min channel spread: flat greys and blacks are not banners
	bandMaxSkew  = 3  // the band's top edge must be level across the picture
	bandMaxRough = 6  // average colour change between neighbouring rows inside the band
	bandMinEdge  = 40 // how different the photo just above the band must be, in some channel
)

// sponsorBandTop returns the row where a bottom sponsor band starts, or -1.
func sponsorBandTop(img image.Image) int {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 200 || h < 150 {
		return -1
	}
	ref := rgbAt(img, b.Min.X+2, b.Max.Y-3)
	if sat(ref) < bandMinSat {
		return -1
	}
	// Probe several columns near both side edges; each must be band-coloured
	// from the bottom up. Where the photo above happens to be the band's own
	// colour a column runs on past the band, so the band's top is the lowest
	// place a column stops, and at least two columns must stop there.
	cols := []int{2, 8, 16, w - 17, w - 9, w - 3}
	stops := make([]int, 0, len(cols))
	top := -1
	for _, c := range cols {
		if !near(rgbAt(img, b.Min.X+c, b.Max.Y-3), ref) {
			return -1
		}
		y := b.Max.Y - 3
		for y > b.Min.Y && near(rgbAt(img, b.Min.X+c, y-1), ref) {
			y--
		}
		stops = append(stops, y)
		if y > top {
			top = y
		}
	}
	left, right, rough, steps := 0, 0, 0, 0
	for i, y := range stops {
		if top-y > bandMaxSkew {
			continue
		}
		x := b.Min.X + cols[i]
		// A printed band ends in a sharp line: a few rows up, the photo is
		// clearly another colour. Grass or carpet fades out gradually.
		if y-4 < b.Min.Y || maxDiff(rgbAt(img, x, y-4), ref) < bandMinEdge {
			continue
		}
		if i < len(cols)/2 {
			left++
		} else {
			right++
		}
		// A printed band is flat; texture that merely has the right colour
		// changes from one row to the next.
		for yy := top + 2; yy < b.Max.Y-3; yy++ {
			p, q := rgbAt(img, x, yy), rgbAt(img, x, yy+1)
			rough += abs(p.r-q.r) + abs(p.g-q.g) + abs(p.b-q.b)
			steps++
		}
	}
	// The line must show on both sides of the picture.
	if left == 0 || right == 0 || float64(rough)/float64(steps) > bandMaxRough {
		return -1
	}
	share := float64(b.Max.Y-top) / float64(h)
	if share < bandMinShare || share > bandMaxShare {
		return -1
	}
	// JPEG smears the band's edge into the row above it; cut that row too.
	return top - 1
}

// TrimSponsorBand rewrites the image file at path without its bottom sponsor
// band. It reports whether anything was cut. Formats the standard library
// cannot decode (WebP) are left alone.
func TrimSponsorBand(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return false, nil
	}
	top := sponsorBandTop(img)
	if top < 0 {
		return false, nil
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), top-b.Min.Y))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)

	var buf bytes.Buffer
	if format == "png" {
		err = png.Encode(&buf, out)
	} else {
		err = jpeg.Encode(&buf, out, &jpeg.Options{Quality: 90})
	}
	if err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return false, err
	}
	return true, nil
}

// sweepMarker records that every file already on disk has been checked, so
// the sweep runs once per volume rather than on every start.
const sweepMarker = ".sponsor-bands-trimmed-v1"

// TrimExisting checks every cached image once and cuts sponsor bands from
// the ones that have them. New downloads are trimmed as they arrive.
func (s *Store) TrimExisting() (checked, trimmed int) {
	if s == nil || s.Dir == "" {
		return 0, 0
	}
	marker := filepath.Join(s.Dir, sweepMarker)
	if _, err := os.Stat(marker); err == nil {
		return 0, 0
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".tmp") {
			continue
		}
		checked++
		if ok, _ := TrimSponsorBand(filepath.Join(s.Dir, name)); ok {
			trimmed++
		}
	}
	_ = os.WriteFile(marker, nil, 0o644)
	return checked, trimmed
}

type rgb struct{ r, g, b int }

func rgbAt(img image.Image, x, y int) rgb {
	r, g, b, _ := img.At(x, y).RGBA()
	return rgb{int(r >> 8), int(g >> 8), int(b >> 8)}
}

func near(a, b rgb) bool {
	return abs(a.r-b.r) <= bandTol && abs(a.g-b.g) <= bandTol && abs(a.b-b.b) <= bandTol
}

func maxDiff(a, b rgb) int {
	d := abs(a.r - b.r)
	if v := abs(a.g - b.g); v > d {
		d = v
	}
	if v := abs(a.b - b.b); v > d {
		d = v
	}
	return d
}

func sat(c rgb) int {
	hi, lo := c.r, c.r
	for _, v := range []int{c.g, c.b} {
		if v > hi {
			hi = v
		}
		if v < lo {
			lo = v
		}
	}
	return hi - lo
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
