// Command indd-bridge makes an Adobe InDesign document (.indd) of an e-paper
// page. It runs on a Windows computer that has InDesign, and listens only on
// that computer (127.0.0.1): the e-paper's "Id" button sends it the page as
// the browser drew it (every frame's place and size, its text and type, the
// photos and rules) and gets the .indd back. InDesign itself builds and
// saves the document, driven by page.jsx through its scripting interface.
//
//	go build -o indd-bridge.exe ./cmd/indd-bridge
//	indd-bridge.exe            (keep it running while you use the e-paper)
package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

//go:embed page.jsx
var pageJSX string

// The e-paper sites allowed to use the bridge (the hosted portal and the
// local one); -origins adds more.
var defaultOrigins = "https://bangla-news-portal.onrender.com,http://localhost:8081,http://127.0.0.1:8081"

func main() {
	addr := flag.String("addr", "127.0.0.1:8095", "where to listen (keep it on 127.0.0.1)")
	origins := flag.String("origins", "", "more e-paper sites allowed to ask, comma separated")
	save := flag.String("save", "", "also keep a copy of every .indd made in this folder")
	verbose := flag.Bool("v", false, "log every request")
	flag.Parse()
	b := &bridge{allowed: map[string]bool{}, save: *save, verbose: *verbose}
	for _, o := range strings.Split(defaultOrigins+","+*origins, ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			b.allowed[o] = true
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", b.health)
	mux.HandleFunc("/indd", b.indd)
	log.Printf("InDesign bridge on http://%s for %d sites", *addr, len(b.allowed))
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

type bridge struct {
	allowed map[string]bool
	save    string
	verbose bool
	mu      sync.Mutex // InDesign makes one document at a time
}

// cors lets the e-paper's own pages call the bridge, and no other site: the
// browser sends the page's origin, and Chrome asks before a public page may
// reach this computer (Private Network Access).
func (b *bridge) cors(w http.ResponseWriter, r *http.Request) bool {
	o := r.Header.Get("Origin")
	if b.verbose {
		log.Printf("%s %s from %q", r.Method, r.URL.Path, o)
	}
	if !b.allowed[o] {
		log.Printf("refused %s %s from %q (not an e-paper site; see -origins)", r.Method, r.URL.Path, o)
		http.Error(w, "this site may not use the InDesign bridge", http.StatusForbidden)
		return false
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", o)
	h.Set("Vary", "Origin")
	h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type")
	h.Set("Access-Control-Allow-Private-Network", "true")
	h.Set("Access-Control-Expose-Headers", "Content-Disposition")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	return true
}

func (b *bridge) health(w http.ResponseWriter, r *http.Request) {
	if !b.cors(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (b *bridge) indd(w http.ResponseWriter, r *http.Request) {
	if !b.cors(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST the page", http.StatusMethodNotAllowed)
		return
	}
	var lay layout
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 40<<20))
	if err := dec.Decode(&lay); err != nil {
		http.Error(w, "the page could not be read: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := lay.check(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dir, err := os.MkdirTemp("", "indd-bridge-")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(dir)
	start := time.Now()
	convert := fetchImages(r.Context(), &lay, dir)
	b.mu.Lock()
	out, err := makeINDD(r.Context(), &lay, dir, convert)
	b.mu.Unlock()
	if err != nil {
		log.Printf("%s: %v", lay.Name, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := os.ReadFile(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("%s.indd: %d frames, %d KB, %s", lay.Name, len(lay.Items), len(data)/1024, time.Since(start).Round(time.Second))
	if b.save != "" {
		if err := os.MkdirAll(b.save, 0o755); err == nil {
			os.WriteFile(filepath.Join(b.save, lay.Name+".indd"), data, 0o644)
		}
	}
	w.Header().Set("Content-Type", "application/x-indesign")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.indd"`, lay.Name))
	w.Write(data)
}

// --- the page ------------------------------------------------------------------

// layout is a page as the e-paper drew it, in its own pixels (1000 wide).
type layout struct {
	Name    string     `json:"name"`
	Width   float64    `json:"width"`
	Height  float64    `json:"height"`
	Margins [4]float64 `json:"margins"` // top, right, bottom, left
	Items   []item     `json:"items"`
	Out     string     `json:"out"` // set by the bridge: where InDesign saves
	MM      float64    `json:"mm"`  // set by the bridge: millimetres per pixel
}

// item is one frame: text, img, line or rect.
type item struct {
	K      string  `json:"k"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
	X2     float64 `json:"x2"`
	Y2     float64 `json:"y2"`
	Fill   string  `json:"fill,omitempty"`
	Color  string  `json:"color,omitempty"`
	SW     float64 `json:"sw,omitempty"`
	Double bool    `json:"double,omitempty"`
	Src    string  `json:"src,omitempty"`
	Data   string  `json:"data,omitempty"`
	File   string  `json:"file,omitempty"` // set by the bridge: the photo on disk
	Cols   int     `json:"cols,omitempty"`
	Gap    float64 `json:"gap,omitempty"`
	Fit    bool    `json:"fit,omitempty"` // grows to hold its text
	One    bool    `json:"one,omitempty"` // never wraps: grows sideways too
	HScale float64 `json:"hscale,omitempty"`
	Paras  []para  `json:"paras,omitempty"`
}

// para is a paragraph and its type.
type para struct {
	T      string   `json:"t"`
	Font   string   `json:"font"`
	Weight int      `json:"weight"`
	Italic bool     `json:"italic,omitempty"`
	Size   float64  `json:"size"`
	Lead   float64  `json:"lead"`
	Align  string   `json:"align"`
	Color  string   `json:"color"`
	Indent float64  `json:"indent,omitempty"`
	Track  float64  `json:"track,omitempty"`
	Before float64  `json:"before,omitempty"`
	After  float64  `json:"after,omitempty"`
	Rules  bool     `json:"rules,omitempty"`
	Fonts  []string `json:"fonts,omitempty"`  // set by the bridge: fonts to try, in order
	Cond   int      `json:"cond,omitempty"`   // set by the bridge: from this one on, a stand-in for a narrower face …
	Narrow float64  `json:"narrow,omitempty"` // … set this much narrower
}

var (
	colourRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)
	nameRe   = regexp.MustCompile(`[^A-Za-z0-9_-]+`)
)

// check makes sure the page is what the e-paper sends, and nothing else
// reaches InDesign: known kinds, sane numbers, plain colours, web photos.
func (l *layout) check() error {
	l.Name = strings.Trim(nameRe.ReplaceAllString(l.Name, "_"), "_")
	if l.Name == "" {
		l.Name = "DailyStar_page"
	}
	if len(l.Name) > 90 {
		l.Name = l.Name[:90]
	}
	if !(l.Width >= 100 && l.Width <= 5000 && l.Height >= 100 && l.Height <= 8000) {
		return errors.New("the page has no size")
	}
	if len(l.Items) == 0 || len(l.Items) > 4000 {
		return errors.New("the page has no frames, or too many")
	}
	for _, m := range l.Margins {
		if !num(m) {
			return errors.New("bad margins")
		}
	}
	text := 0
	for i := range l.Items {
		it := &l.Items[i]
		for _, v := range []float64{it.X, it.Y, it.W, it.H, it.X2, it.Y2, it.SW, it.Gap, it.HScale} {
			if !num(v) {
				return fmt.Errorf("frame %d: bad number", i)
			}
		}
		if (it.Fill != "" && !colourRe.MatchString(it.Fill)) || (it.Color != "" && !colourRe.MatchString(it.Color)) {
			return fmt.Errorf("frame %d: bad colour", i)
		}
		switch it.K {
		case "rect", "line":
		case "img":
			if it.Data != "" {
				if !strings.HasPrefix(it.Data, "data:image/png;base64,") {
					return fmt.Errorf("frame %d: drawings come as PNG", i)
				}
			} else if u, err := url.Parse(it.Src); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				return fmt.Errorf("frame %d: photos come from the web", i)
			}
		case "text":
			if it.Cols < 0 || it.Cols > 12 || len(it.Paras) == 0 || len(it.Paras) > 400 {
				return fmt.Errorf("frame %d: bad text frame", i)
			}
			for j := range it.Paras {
				p := &it.Paras[j]
				text += len(p.T)
				if !num(p.Size) || p.Size <= 0 || p.Size > 600 || !num(p.Lead) || !num(p.Indent) || !num(p.Track) || !num(p.Before) || !num(p.After) {
					return fmt.Errorf("frame %d: bad type", i)
				}
				if p.Color != "" && !colourRe.MatchString(p.Color) {
					return fmt.Errorf("frame %d: bad colour", i)
				}
				switch p.Align {
				case "left", "right", "center", "justify":
				default:
					p.Align = "left"
				}
				// A line break inside a paragraph stays a forced line break
				// ("\n" in InDesign); "\r" would start a new paragraph.
				p.T = strings.NewReplacer("\r\n", "\n", "\r", "\n", " ", "\n").Replace(p.T)
				p.Fonts, p.Cond, p.Narrow = fontsFor(p.Font, p.Weight, p.Italic)
			}
		default:
			return fmt.Errorf("frame %d: unknown kind %q", i, it.K)
		}
	}
	if text > 4<<20 {
		return errors.New("too much text")
	}
	return nil
}

func num(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v > -100000 && v < 100000 }

// fontsFor lists the fonts to try for the e-paper's type, best first: the
// Daily Star's own faces, then ones every Windows computer has. A stand-in
// for a condensed face (from the index returned) is set narrower by the
// factor returned, so its lines break about where the real face's would.
func fontsFor(family string, weight int, italic bool) ([]string, int, float64) {
	f := strings.ToLower(family)
	bold := weight >= 600
	plain := func(fam string) string {
		switch {
		case bold && italic:
			return fam + "\tBold Italic"
		case bold:
			return fam + "\tBold"
		case italic:
			return fam + "\tItalic"
		}
		return fam + "\tRegular"
	}
	switch {
	case strings.Contains(f, "denton"):
		st := "Regular"
		switch {
		case weight <= 300:
			st = "Light"
		case weight >= 650:
			st = "Bold"
		case weight >= 500:
			st = "Medium"
		}
		if italic {
			if st == "Regular" {
				st = "Italic"
			} else {
				st += " Italic"
			}
		}
		return []string{"Denton Condensed\t" + st, "Denton Condensed Test\t" + st, plain("Times New Roman")}, 2, 0.86
	case strings.Contains(f, "extra condensed"):
		st := "Regular"
		switch {
		case weight >= 650:
			st = "Bold"
		case weight >= 550:
			st = "SemiBold"
		}
		return []string{"Fira Sans Extra Condensed\t" + st, plain("Arial Narrow"), plain("Arial")}, 1, 0.9
	case strings.Contains(f, "fira"):
		return []string{plain("Fira Sans"), plain("Arial")}, 0, 1
	case strings.Contains(f, "tinos"), strings.Contains(f, "times"):
		return []string{plain("Tinos"), plain("Times New Roman")}, 0, 1
	}
	return []string{plain("Times New Roman")}, 0, 1
}

// --- photos -------------------------------------------------------------------

// fetchImages saves the page's photos and drawings beside the document.
// InDesign takes JPEG, PNG and GIF; a photo in another format (WebP, AVIF)
// is listed to be turned into JPEG by Windows first. A photo that cannot
// be had leaves its frame empty.
func fetchImages(ctx context.Context, l *layout, dir string) []string {
	var convert []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 6)
	client := &http.Client{Timeout: 40 * time.Second}
	for i := range l.Items {
		it := &l.Items[i]
		if it.K != "img" {
			continue
		}
		base := filepath.Join(dir, fmt.Sprintf("img%03d", i))
		if it.Data != "" {
			raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(it.Data, "data:image/png;base64,"))
			if err == nil && os.WriteFile(base+".png", raw, 0o600) == nil {
				it.File = base + ".png"
			}
			it.Data = ""
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			file, odd, err := download(ctx, client, it.Src, base)
			if err != nil {
				log.Printf("photo %s: %v", it.Src, err)
				return
			}
			it.File = file
			if odd {
				it.File = base + ".jpg" // what Windows turns it into
				mu.Lock()
				convert = append(convert, file)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return convert
}

func download(ctx context.Context, client *http.Client, src, base string) (file string, odd bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return "", false, err
	}
	// Ask for a format InDesign can place.
	req.Header.Set("Accept", "image/jpeg,image/png,image/gif;q=0.9,*/*;q=0.2")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) indd-bridge/1.0")
	res, err := client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 30<<20))
	if err != nil {
		return "", false, err
	}
	ext := ".jpg"
	switch http.DetectContentType(raw) {
	case "image/jpeg":
	case "image/png":
		ext = ".png"
	case "image/gif":
		ext = ".gif"
	default:
		odd = true
		ext = ".src"
	}
	file = base + ext
	return file, odd, os.WriteFile(file, raw, 0o600)
}

// --- InDesign -----------------------------------------------------------------

// makeINDD has InDesign build the page and save it, and returns the file.
func makeINDD(ctx context.Context, l *layout, dir string, convert []string) (string, error) {
	l.Out = filepath.Join(dir, l.Name+".indd")
	l.MM = 297 / l.Width // the page is A3, 297 mm wide
	data, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	jsx := filepath.Join(dir, "page.jsx")
	// The page goes in as data (JSON is a JavaScript literal), ahead of the
	// script that lays it out.
	if err := os.WriteFile(jsx, []byte("var L = "+string(data)+";\n"+pageJSX), 0o600); err != nil {
		return "", err
	}
	ps := filepath.Join(dir, "run.ps1")
	if err := os.WriteFile(ps, []byte(runScript(jsx, convert)), 0o600); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", ps).CombinedOutput()
	res := strings.TrimSpace(string(out))
	if err != nil || !strings.HasSuffix(res, "OK") {
		if res == "" && err != nil {
			res = err.Error()
		}
		return "", fmt.Errorf("InDesign: %s", lastLine(res))
	}
	if _, err := os.Stat(l.Out); err != nil {
		return "", errors.New("InDesign did not save the page")
	}
	return l.Out, nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

// runScript is the PowerShell that turns odd photos into JPEG with Windows'
// own decoders, then has InDesign (through its COM interface) run the page
// script.
func runScript(jsx string, convert []string) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	var list []string
	for _, c := range convert {
		list = append(list, q(c))
	}
	return `$ErrorActionPreference = 'Stop'
$convert = @(` + strings.Join(list, ",") + `)
if ($convert.Count -gt 0) {
  Add-Type -AssemblyName PresentationCore
  foreach ($f in $convert) {
    try {
      $in = [IO.File]::OpenRead($f)
      $dec = [Windows.Media.Imaging.BitmapDecoder]::Create($in, 'None', 'OnLoad')
      $enc = New-Object Windows.Media.Imaging.JpegBitmapEncoder
      $enc.QualityLevel = 92
      $enc.Frames.Add([Windows.Media.Imaging.BitmapFrame]::Create($dec.Frames[0]))
      $in.Close()
      $out = [IO.File]::Create([IO.Path]::ChangeExtension($f, '.jpg'))
      $enc.Save($out)
      $out.Close()
    } catch { if ($in) { $in.Close() } }
  }
}
$id = $null
foreach ($p in @('InDesign.Application', 'InDesign.Application.CC.2018')) {
  try { $id = New-Object -ComObject $p; break } catch {}
}
if (-not $id) { 'Adobe InDesign is not installed on this computer'; exit 1 }
$src = [IO.File]::ReadAllText(` + q(jsx) + `, [Text.Encoding]::UTF8)
$id.DoScript($src, 1246973031)
`
}
