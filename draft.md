# SOURCE CODE

## go.mod
```go
module svgextract

go 1.24.4

require gopkg.in/yaml.v3 v3.0.1

```
---

## go.sum
```text
gopkg.in/check.v1 v0.0.0-20161208181325-20d25e280405 h1:yhCVgyC4o1eVCa2tZl7eS0r+SDo693bJlVdllGtEeKM=
gopkg.in/check.v1 v0.0.0-20161208181325-20d25e280405/go.mod h1:Co6ibVJAznAaIkqp8huTwlJQCZ016jof/cbN4VW5Yz0=
gopkg.in/yaml.v3 v3.0.1 h1:fxVm/GzAzEWqLHuvctI91KS9hhNmmWOoWu0XTYJS7CA=
gopkg.in/yaml.v3 v3.0.1/go.mod h1:K4uyk7z7BCEPqu6E+C64Yfv1cQ7kz7rIZviUmN+EgEM=

```
---

## main.go
```go
package main

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed static/index.html static/style.css static/script.js
var staticFiles embed.FS

const (
	ktpPortraitW  = 638
	ktpPortraitH  = 1012
	ktpLandscapeW = 1012
	ktpLandscapeH = 638
	dpi           = 300
)

type Job struct {
	ID     string
	Status string
	Files  []string
	Error  string
	mu     sync.Mutex
}

type JobStore struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func (s *JobStore) New(id string) *Job {
	j := &Job{ID: id, Status: "pending"}
	s.mu.Lock()
	s.jobs[id] = j
	s.mu.Unlock()
	return j
}

func (s *JobStore) Get(id string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

var store = &JobStore{jobs: make(map[string]*Job)}

var allowedIDs = map[string]bool{
	"logo_instansi": true,
	"foto_user":     true,
	"qr_user":       true,
}

type SVGNode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Nodes   []SVGNode  `xml:",any"`
}

type SVGMatrix struct{ a, b, c, d, e, f float64 }

func svgId() SVGMatrix { return SVGMatrix{1, 0, 0, 1, 0, 0} }

func svgMul(m1, m2 SVGMatrix) SVGMatrix {
	return SVGMatrix{
		a: m2.a*m1.a + m2.c*m1.b, b: m2.b*m1.a + m2.d*m1.b,
		c: m2.a*m1.c + m2.c*m1.d, d: m2.b*m1.c + m2.d*m1.d,
		e: m2.a*m1.e + m2.c*m1.f + m2.e, f: m2.b*m1.e + m2.d*m1.f + m2.f,
	}
}

func (m SVGMatrix) apply(x, y float64) (float64, float64) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}

func parseTransform(s string) SVGMatrix {
	res := svgId()
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		op, cl := strings.Index(s, "("), strings.Index(s, ")")
		if op == -1 || cl == -1 {
			break
		}
		name := strings.TrimSpace(s[:op])
		var args []float64
		for _, p := range strings.FieldsFunc(s[op+1:cl], func(r rune) bool { return r == ',' || r == ' ' }) {
			if v, err := strconv.ParseFloat(p, 64); err == nil {
				args = append(args, v)
			}
		}
		var m SVGMatrix
		switch name {
		case "translate":
			tx, ty := args[0], 0.0
			if len(args) > 1 {
				ty = args[1]
			}
			m = SVGMatrix{1, 0, 0, 1, tx, ty}
		case "scale":
			sx, sy := args[0], args[0]
			if len(args) > 1 {
				sy = args[1]
			}
			m = SVGMatrix{sx, 0, 0, sy, 0, 0}
		case "matrix":
			if len(args) == 6 {
				m = SVGMatrix{args[0], args[1], args[2], args[3], args[4], args[5]}
			} else {
				m = svgId()
			}
		case "rotate":
			th := args[0] * math.Pi / 180
			co, si := math.Cos(th), math.Sin(th)
			rot := SVGMatrix{co, si, -si, co, 0, 0}
			if len(args) == 3 {
				cx, cy := args[1], args[2]
				m = svgMul(svgMul(SVGMatrix{1, 0, 0, 1, -cx, -cy}, rot), SVGMatrix{1, 0, 0, 1, cx, cy})
			} else {
				m = rot
			}
		default:
			m = svgId()
		}
		res = svgMul(res, m)
		s = strings.TrimSpace(s[cl+1:])
	}
	return res
}

func svgAttr(n SVGNode, name string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func svgFloat(s string) float64 {
	s = strings.TrimSuffix(strings.TrimSpace(s), "px")
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func r1(v float64) float64 { return math.Round(v*10) / 10 }

func tBox(m SVGMatrix, x0, y0, x1, y1 float64) (float64, float64, float64, float64) {
	cs := [][2]float64{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range cs {
		px, py := m.apply(c[0], c[1])
		minX, minY = math.Min(minX, px), math.Min(minY, py)
		maxX, maxY = math.Max(maxX, px), math.Max(maxY, py)
	}
	return minX, minY, maxX, maxY
}

type Extracted map[string]interface{}

func walkSVG(n SVGNode, pm SVGMatrix, res map[string]Extracted) {
	m := pm
	if t := svgAttr(n, "transform"); t != "" {
		m = svgMul(pm, parseTransform(t))
	}
	id, tag := svgAttr(n, "id"), n.XMLName.Local
	if id != "" && allowedIDs[id] {
		switch tag {
		case "rect", "image":
			x, y := svgFloat(svgAttr(n, "x")), svgFloat(svgAttr(n, "y"))
			w, h := svgFloat(svgAttr(n, "width")), svgFloat(svgAttr(n, "height"))
			minX, minY, maxX, maxY := tBox(m, x, y, x+w, y+h)
			res[id] = Extracted{"type": "rect", "x": r1(minX), "y": r1(minY), "width": r1(maxX - minX), "height": r1(maxY - minY)}
		case "circle", "ellipse":
			cx, cy := svgFloat(svgAttr(n, "cx")), svgFloat(svgAttr(n, "cy"))
			r := svgFloat(svgAttr(n, "r"))
			if r == 0 {
				r = svgFloat(svgAttr(n, "rx"))
			}
			minX, minY, maxX, maxY := tBox(m, cx-r, cy-r, cx+r, cy+r)
			res[id] = Extracted{"type": "circle", "center_x": r1((minX + maxX) / 2), "center_y": r1((minY + maxY) / 2), "diameter": r1(maxX - minX)}
		}
	}
	for _, child := range n.Nodes {
		walkSVG(child, m, res)
	}
}

func extractSVGConfig(svgData []byte) (map[string]Extracted, error) {
	var root SVGNode
	if err := xml.Unmarshal(svgData, &root); err != nil {
		return nil, err
	}
	res := make(map[string]Extracted)
	walkSVG(root, svgId(), res)
	return res, nil
}

var textPlaceholders = []string{
	"nama_instansi", "nama_kartu", "uid_kartu",
	"nama_user", "username", "hp_instansi",
	"ig_instansi", "email_instansi", "web_instansi",
	"alamat_instansi",
}

func injectSVG(svgData []byte, row map[string]string, images map[string][]byte) ([]byte, error) {
	content := string(svgData)
	for _, key := range textPlaceholders {
		placeholder := "%VAR_" + key + "%"
		val := row[key]
		if val == "" {
			val = row[strings.ReplaceAll(key, "_", "")]
		}
		content = strings.ReplaceAll(content, placeholder, escXML(val))
	}
	for id, imgData := range images {
		if len(imgData) == 0 {
			continue
		}
		b64 := base64.StdEncoding.EncodeToString(imgData)
		dataURI := "data:" + detectMime(imgData) + ";base64," + b64
		content = embedImageInSVG(content, id, dataURI)
	}
	return []byte(content), nil
}

func detectMime(data []byte) string {
	t := http.DetectContentType(data)
	if strings.HasPrefix(t, "image/") {
		return t
	}
	return "image/png"
}

func embedImageInSVG(svg, id, dataURI string) string {
	idAttr := fmt.Sprintf(`id="%s"`, id)
	for _, openTag := range []string{"<image", "<rect"} {
		idx := 0
		for {
			pos := strings.Index(svg[idx:], openTag)
			if pos == -1 {
				break
			}
			pos += idx
			after := pos + len(openTag)
			if after >= len(svg) || !isWS(rune(svg[after])) {
				idx = pos + 1
				continue
			}
			end := findTagEnd(svg[pos:])
			if end == -1 {
				break
			}
			end += pos
			tagContent := svg[pos:end]
			if !strings.Contains(tagContent, idAttr) {
				idx = pos + 1
				continue
			}
			var newTag string
			if openTag == "<image" {
				newTag = replAttr(tagContent, "href", dataURI)
				newTag = replAttr(newTag, "xlink:href", dataURI)
			} else {
				x, y := exAttr(tagContent, "x"), exAttr(tagContent, "y")
				w, h := exAttr(tagContent, "width"), exAttr(tagContent, "height")
				newTag = fmt.Sprintf(`<image id="%s" x="%s" y="%s" width="%s" height="%s" preserveAspectRatio="xMidYMid slice" href="%s" xlink:href="%s" />`,
					id, x, y, w, h, dataURI, dataURI)
				tail := svg[end:]
				if endR := strings.Index(tail, "</rect>"); endR != -1 && strings.TrimSpace(tail[:min(endR+1, len(tail))]) == "" {
					end += endR + len("</rect>")
				}
			}
			return svg[:pos] + newTag + svg[end:]
		}
	}
	return svg
}

func isWS(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

func findTagEnd(s string) int {
	inQ := rune(0)
	for i, r := range s {
		if inQ != 0 {
			if r == inQ {
				inQ = 0
			}
			continue
		}
		if r == '"' || r == '\'' {
			inQ = r
			continue
		}
		if r == '>' {
			return i + 1
		}
	}
	return -1
}

func replAttr(tag, attr, val string) string {
	for _, q := range []string{`"`, `'`} {
		pat := attr + `=` + q
		s := strings.Index(tag, pat)
		if s == -1 {
			continue
		}
		vs := s + len(pat)
		ve := strings.Index(tag[vs:], q)
		if ve == -1 {
			continue
		}
		return tag[:s] + attr + `="` + val + `"` + tag[vs+ve+1:]
	}
	return strings.TrimSuffix(tag, ">") + ` ` + attr + `="` + val + `">`
}

func exAttr(tag, attr string) string {
	for _, q := range []string{`"`, `'`} {
		pat := attr + `=` + q
		s := strings.Index(tag, pat)
		if s == -1 {
			continue
		}
		s += len(pat)
		e := strings.Index(tag[s:], q)
		if e == -1 {
			continue
		}
		return tag[s : s+e]
	}
	return ""
}

func escXML(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

func renderPNG(svgPath, pngPath string, portrait bool) error {
	w, h := ktpLandscapeW, ktpLandscapeH
	if portrait {
		w, h = ktpPortraitW, ktpPortraitH
	}
	cmd := exec.Command("inkscape",
		"--export-type=png",
		"--export-filename="+pngPath,
		fmt.Sprintf("--export-width=%d", w),
		fmt.Sprintf("--export-height=%d", h),
		fmt.Sprintf("--export-dpi=%d", dpi),
		svgPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("inkscape: %v - %s", err, out)
	}
	return nil
}

func parseCSV(data []byte) ([]map[string]string, []string, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.TrimLeadingSpace = true
	headers, err := r.Read()
	if err != nil {
		return nil, nil, err
	}
	for i, h := range headers {
		headers[i] = strings.TrimSpace(h)
	}
	var rows []map[string]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		row := make(map[string]string)
		for i, h := range headers {
			if i < len(rec) {
				row[h] = strings.TrimSpace(rec[i])
			}
		}
		rows = append(rows, row)
	}
	return rows, headers, nil
}

func handleStatic(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "static/index.html"
	} else {
		path = "static/" + path
	}
	data, err := staticFiles.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch filepath.Ext(path) {
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	w.Write(data)
}

func handleTemplateCSV(w http.ResponseWriter, r *http.Request) {
	cols := []string{"uid_kartu", "nama_user", "username", "nama_instansi", "nama_kartu",
		"hp_instansi", "ig_instansi", "email_instansi", "web_instansi", "alamat_instansi",
		"foto_user", "qr_user"}
	w.Header().Set("Content-Disposition", `attachment; filename="template_data.csv"`)
	w.Header().Set("Content-Type", "text/csv")
	cw := csv.NewWriter(w)
	cw.Write(cols)
	cw.Write([]string{"KRT-0001", "Nama Lengkap", "username01", "Nama Instansi", "Kartu Pelajar",
		"+62 812 3456 7890", "@instansi", "info@instansi.sch.id", "https://instansi.sch.id",
		"Jl. Contoh No. 1, Kota", "foto/username01.png", "qr/username01.png"})
	cw.Flush()
}

func handleExtractConfig(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(32 << 20)
	f, _, err := r.FormFile("svg")
	if err != nil {
		jsonErr(w, "file SVG tidak ditemukan: "+err.Error(), 400)
		return
	}
	defer f.Close()
	svgData, _ := io.ReadAll(f)
	cfg, err := extractSVGConfig(svgData)
	if err != nil {
		jsonErr(w, "gagal parse SVG: "+err.Error(), 500)
		return
	}
	yamlOut, _ := yaml.Marshal(cfg)
	w.Header().Set("Content-Disposition", `attachment; filename="config.yaml"`)
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Write(yamlOut)
}

func handleGenerate(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(128 << 20)
	portrait := r.FormValue("orientation") != "landscape"
	svgFile, _, err := r.FormFile("svg")
	if err != nil {
		jsonErr(w, "SVG tidak ditemukan", 400)
		return
	}
	svgData, _ := io.ReadAll(svgFile)
	svgFile.Close()
	csvFile, _, err := r.FormFile("csv")
	if err != nil {
		jsonErr(w, "CSV tidak ditemukan", 400)
		return
	}
	csvData, _ := io.ReadAll(csvFile)
	csvFile.Close()
	rows, _, err := parseCSV(csvData)
	if err != nil {
		jsonErr(w, "gagal parse CSV: "+err.Error(), 400)
		return
	}
	globalImages := map[string][]byte{}
	if lf, _, err := r.FormFile("logo_instansi"); err == nil {
		globalImages["logo_instansi"], _ = io.ReadAll(lf)
		lf.Close()
	}
	fotoMap := readMultipleFiles(r, "foto_user")
	qrMap := readMultipleFiles(r, "qr_user")
	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	job := store.New(jobID)
	tmpDir, _ := os.MkdirTemp("", jobID)
	go func() {
		job.mu.Lock()
		job.Status = "processing"
		job.mu.Unlock()
		var outFiles []string
		for _, row := range rows {
			uid := row["uid_kartu"]
			if uid == "" {
				uid = fmt.Sprintf("row_%d", time.Now().UnixNano())
			}
			imgs := map[string][]byte{}
			for k, v := range globalImages {
				imgs[k] = v
			}
			if data := findImage(fotoMap, row["foto_user"], row["username"]); data != nil {
				imgs["foto_user"] = data
			}
			if data := findImage(qrMap, row["qr_user"], row["username"]); data != nil {
				imgs["qr_user"] = data
			}
			injected, err := injectSVG(svgData, row, imgs)
			if err != nil {
				continue
			}
			svgPath := filepath.Join(tmpDir, uid+".svg")
			pngPath := filepath.Join(tmpDir, uid+".png")
			os.WriteFile(svgPath, injected, 0644)
			if err := renderPNG(svgPath, pngPath, portrait); err != nil {
				continue
			}
			os.Remove(svgPath)
			outFiles = append(outFiles, pngPath)
		}
		job.mu.Lock()
		job.Status = "done"
		job.Files = outFiles
		job.mu.Unlock()
	}()
	jsonOK(w, map[string]string{"job_id": jobID})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("job_id")
	job := store.Get(id)
	if job == nil {
		jsonErr(w, "job tidak ditemukan", 404)
		return
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	jsonOK(w, map[string]interface{}{
		"status": job.Status,
		"total":  len(job.Files),
		"error":  job.Error,
	})
}

func handleDownloadOne(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("job_id")
	name := filepath.Base(r.URL.Query().Get("file"))
	job := store.Get(id)
	if job == nil {
		jsonErr(w, "job tidak ditemukan", 404)
		return
	}
	job.mu.Lock()
	files := job.Files
	job.mu.Unlock()
	for _, f := range files {
		if filepath.Base(f) == name {
			w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
			w.Header().Set("Content-Type", "image/png")
			http.ServeFile(w, r, f)
			return
		}
	}
	jsonErr(w, "file tidak ditemukan", 404)
}

func handleDownloadZip(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JobID string   `json:"job_id"`
		Files []string `json:"files"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	job := store.Get(req.JobID)
	if job == nil {
		jsonErr(w, "job tidak ditemukan", 404)
		return
	}
	job.mu.Lock()
	allFiles := job.Files
	job.mu.Unlock()
	selected := allFiles
	if len(req.Files) > 0 {
		sel := make(map[string]bool)
		for _, f := range req.Files {
			sel[f] = true
		}
		selected = nil
		for _, f := range allFiles {
			if sel[filepath.Base(f)] {
				selected = append(selected, f)
			}
		}
	}
	w.Header().Set("Content-Disposition", `attachment; filename="kartu.zip"`)
	w.Header().Set("Content-Type", "application/zip")
	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, f := range selected {
		fw, err := zw.Create(filepath.Base(f))
		if err != nil {
			continue
		}
		data, _ := os.ReadFile(f)
		fw.Write(data)
	}
}

func handleList(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("job_id")
	job := store.Get(id)
	if job == nil {
		jsonErr(w, "job tidak ditemukan", 404)
		return
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	names := make([]string, len(job.Files))
	for i, f := range job.Files {
		names[i] = filepath.Base(f)
	}
	jsonOK(w, map[string]interface{}{"files": names, "status": job.Status})
}

func readMultipleFiles(r *http.Request, key string) map[string][]byte {
	result := map[string][]byte{}
	if r.MultipartForm == nil {
		return result
	}
	for _, fh := range r.MultipartForm.File[key] {
		f, err := fh.Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(f)
		f.Close()
		if strings.EqualFold(filepath.Ext(fh.Filename), ".zip") {
			extractZipImages(data, result)
			continue
		}
		result[fh.Filename] = data
		result[strings.TrimSuffix(fh.Filename, filepath.Ext(fh.Filename))] = data
	}
	return result
}

func extractZipImages(data []byte, result map[string][]byte) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(f.Name)
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}
		result[name] = content
		result[strings.TrimSuffix(name, filepath.Ext(name))] = content
	}
}

func findImage(m map[string][]byte, pathInCSV, username string) []byte {
	if d, ok := m[filepath.Base(pathInCSV)]; ok {
		return d
	}
	if d, ok := m[pathInCSV]; ok {
		return d
	}
	if d, ok := m[username+".png"]; ok {
		return d
	}
	if d, ok := m[username+".jpg"]; ok {
		return d
	}
	if d, ok := m[username+".jpeg"]; ok {
		return d
	}
	if d, ok := m[username]; ok {
		return d
	}
	return nil
}

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8090"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/template-csv", handleTemplateCSV)
	mux.HandleFunc("/api/extract-config", handleExtractConfig)
	mux.HandleFunc("/api/generate", handleGenerate)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/list", handleList)
	mux.HandleFunc("/api/download", handleDownloadOne)
	mux.HandleFunc("/api/download-zip", handleDownloadZip)
	mux.HandleFunc("/", handleStatic)
	fmt.Printf("Server berjalan di http://localhost:%s\n", port)
	http.ListenAndServe(":"+port, mux)
}

```
---

## static/index.html
```html
<!DOCTYPE html>
<html lang="id">

<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Generator Kartu</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Lexend:wght@300;400;500;600;700&display=swap" rel="stylesheet">
  <link rel="stylesheet" href="style.css">
</head>

<body>
  <header>
    <div class="header-inner">
      <div class="header-brand">
        <div class="header-icon">K</div>
        <div>
          <h1>Generator Kartu Otomatis</h1>
          <span>Inkscape &middot; 300 DPI &middot; Ukuran KTP</span>
        </div>
      </div>
    </div>
  </header>

  <main class="container">

    <div class="card">
      <div class="card-header">
        <span class="badge">1</span>
        <h2>Unduh Template</h2>
      </div>
      <p class="hint">
        Unduh template CSV lalu isi dengan data. Kolom <code>foto_user</code> dan <code>qr_user</code>
        diisi nama file gambar yang di-upload di langkah 5.
      </p>
      <div class="download-row">
        <a href="/api/template-csv" class="btn btn-outline">Unduh template_data.csv</a>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <span class="badge">2</span>
        <h2>Upload Data CSV</h2>
      </div>
      <div class="drop-zone" id="dzCsv" onclick="document.getElementById('csvInput').click()">
        <input type="file" id="csvInput" accept=".csv" onchange="onFileSelect(this,'dzCsv','csvName')">
        <div class="drop-icon">
          <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
            <polyline points="17 8 12 3 7 8" />
            <line x1="12" y1="3" x2="12" y2="15" />
          </svg>
        </div>
        <div class="drop-label">Klik atau seret file CSV ke sini</div>
        <div class="file-name" id="csvName"></div>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <span class="badge">3</span>
        <h2>Upload Template Desain (SVG)</h2>
      </div>
      <div class="drop-zone" id="dzSvg" onclick="document.getElementById('svgInput').click()">
        <input type="file" id="svgInput" accept=".svg" onchange="onFileSelect(this,'dzSvg','svgName')">
        <div class="drop-icon">
          <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
            <rect x="3" y="3" width="18" height="18" rx="2" />
            <circle cx="8.5" cy="8.5" r="1.5" />
            <polyline points="21 15 16 10 5 21" />
          </svg>
        </div>
        <div class="drop-label">Klik atau seret file SVG (Inkscape) ke sini</div>
        <div class="file-name" id="svgName"></div>
      </div>
      <div class="actions">
        <button class="btn btn-outline btn-sm" onclick="generateConfig()">Generate Config dari SVG</button>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <span class="badge">4</span>
        <h2>Orientasi Kartu</h2>
      </div>
      <div class="toggle-group">
        <label class="toggle-item">
          <input type="radio" name="orientation" value="portrait" checked onchange="setOrientation(this)">
          <span class="toggle-label">
            <span class="toggle-icon portrait-icon"></span>
            Potrait
            <small>638 x 1012 px</small>
          </span>
        </label>
        <label class="toggle-item">
          <input type="radio" name="orientation" value="landscape" onchange="setOrientation(this)">
          <span class="toggle-label">
            <span class="toggle-icon landscape-icon"></span>
            Landscape
            <small>1012 x 638 px</small>
          </span>
        </label>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <span class="badge">5</span>
        <h2>Upload Gambar</h2>
      </div>
      <div class="upload-grid">
        <div class="upload-field">
          <label>Logo Instansi</label>
          <p class="hint">1 file, otomatis menyesuaikan ukuran template</p>
          <div class="file-input-wrap">
            <input type="file" id="logoInput" accept="image/*">
          </div>
        </div>
        <div class="upload-field">
          <label>Foto User</label>
          <p class="hint">Multiple file atau ZIP, nama file = username / uid</p>
          <div class="file-input-wrap">
            <input type="file" id="fotoInput" accept="image/*,.zip" multiple>
          </div>
        </div>
        <div class="upload-field">
          <label>QR Code User</label>
          <p class="hint">Multiple file atau ZIP, nama file = username / uid</p>
          <div class="file-input-wrap">
            <input type="file" id="qrInput" accept="image/*,.zip" multiple>
          </div>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <span class="badge">6</span>
        <h2>Generate Kartu</h2>
      </div>
      <div id="alertBox" class="alert"></div>
      <button class="btn btn-primary btn-lg" id="btnGenerate" onclick="startGenerate()">
        Generate Semua Kartu
      </button>
      <div id="progressWrap" class="progress-section" style="display:none">
        <div class="progress-track">
          <div class="progress-fill" id="progressBar"></div>
        </div>
        <div class="status-text" id="statusText">Memproses...</div>
      </div>
    </div>

    <div class="card" id="resultCard" style="display:none">
      <div class="card-header">
        <h2>Hasil Generate</h2>
      </div>
      <div class="result-toolbar">
        <div class="select-actions">
          <button class="btn btn-outline btn-sm" onclick="selectAll(true)">Pilih Semua</button>
          <button class="btn btn-outline btn-sm" onclick="selectAll(false)">Batal Pilih</button>
          <span class="selected-count" id="selectedCount">0 dipilih</span>
        </div>
        <div class="download-actions">
          <button class="btn btn-success btn-sm" onclick="downloadZip('selected')">Unduh Dipilih (.zip)</button>
          <button class="btn btn-success btn-sm" onclick="downloadZip('all')">Unduh Semua (.zip)</button>
        </div>
      </div>
      <div id="resultGrid"></div>
    </div>

  </main>

  <script src="script.js"></script>
</body>

</html>

```
---

## static/script.js
```text
let currentJobId = null;
let currentOrientation = "portrait";
let pollTimer = null;

function onFileSelect(input, zoneId, nameId) {
  const name = input.files[0]?.name || "";
  document.getElementById(nameId).textContent = name;
  document.getElementById(zoneId).classList.toggle("drag", !!name);
}

["dzCsv", "dzSvg"].forEach((id) => {
  const el = document.getElementById(id);
  el.addEventListener("dragover", (e) => {
    e.preventDefault();
    el.classList.add("drag");
  });
  el.addEventListener("dragleave", () => el.classList.remove("drag"));
  el.addEventListener("drop", (e) => {
    e.preventDefault();
    const input = el.querySelector("input[type=file]");
    const nameId = el.id === "dzCsv" ? "csvName" : "svgName";
    input.files = e.dataTransfer.files;
    onFileSelect(input, el.id, nameId);
  });
});

function setOrientation(el) {
  currentOrientation = el.value;
  document.querySelectorAll(".result-item").forEach((i) => {
    i.classList.toggle("landscape", currentOrientation === "landscape");
  });
}

async function generateConfig() {
  const svg = document.getElementById("svgInput").files[0];
  if (!svg) {
    showAlert("Upload file SVG terlebih dahulu.", "error");
    return;
  }
  const fd = new FormData();
  fd.append("svg", svg);
  const res = await fetch("/api/extract-config", { method: "POST", body: fd });
  if (!res.ok) {
    showAlert("Gagal generate config.", "error");
    return;
  }
  const blob = await res.blob();
  dlBlob(blob, "config.yaml");
  showAlert("Config berhasil di-generate dan diunduh.", "success");
}

async function startGenerate() {
  const csv = document.getElementById("csvInput").files[0];
  const svg = document.getElementById("svgInput").files[0];
  if (!csv || !svg) {
    showAlert("Upload file CSV dan SVG terlebih dahulu.", "error");
    return;
  }

  const fd = new FormData();
  fd.append("csv", csv);
  fd.append("svg", svg);
  fd.append("orientation", currentOrientation);

  const logo = document.getElementById("logoInput").files[0];
  if (logo) fd.append("logo_instansi", logo);
  Array.from(document.getElementById("fotoInput").files).forEach((f) => fd.append("foto_user", f));
  Array.from(document.getElementById("qrInput").files).forEach((f) => fd.append("qr_user", f));

  document.getElementById("btnGenerate").disabled = true;
  document.getElementById("progressWrap").style.display = "block";
  document.getElementById("resultCard").style.display = "none";
  document.getElementById("resultGrid").innerHTML = "";
  setProgress(0, "Mengirim data...");
  hideAlert();

  try {
    const res = await fetch("/api/generate", { method: "POST", body: fd });
    const data = await res.json();
    if (!res.ok) {
      showAlert(data.error || "Gagal memulai generate.", "error");
      resetBtn();
      return;
    }
    currentJobId = data.job_id;
    pollStatus();
  } catch (e) {
    showAlert("Koneksi error: " + e.message, "error");
    resetBtn();
  }
}

function pollStatus() {
  if (pollTimer) clearInterval(pollTimer);
  pollTimer = setInterval(async () => {
    const res = await fetch("/api/status?job_id=" + currentJobId);
    const data = await res.json();
    if (data.status === "processing" || data.status === "pending") {
      setProgress(50, "Memproses kartu...");
    } else if (data.status === "done") {
      clearInterval(pollTimer);
      setProgress(100, "Selesai. " + data.total + " kartu berhasil dibuat.");
      loadResults();
      resetBtn();
    } else if (data.status === "error") {
      clearInterval(pollTimer);
      showAlert("Error: " + data.error, "error");
      resetBtn();
    }
  }, 1000);
}

async function loadResults() {
  const res = await fetch("/api/list?job_id=" + currentJobId);
  const data = await res.json();
  const grid = document.getElementById("resultGrid");
  grid.innerHTML = "";
  data.files.forEach((f) => {
    const div = document.createElement("div");
    div.className = "result-item" + (currentOrientation === "landscape" ? " landscape" : "");
    div.dataset.file = f;
    const img = document.createElement("img");
    img.src = "/api/download?job_id=" + currentJobId + "&file=" + f;
    img.loading = "lazy";
    img.alt = f;
    const cb = document.createElement("input");
    cb.type = "checkbox";
    cb.title = "Pilih";
    cb.addEventListener("change", updateCount);
    const label = document.createElement("div");
    label.className = "result-label";
    label.textContent = f;
    const link = document.createElement("a");
    link.href = "/api/download?job_id=" + currentJobId + "&file=" + f;
    link.download = f;
    link.textContent = "Unduh";
    div.append(cb, img, label, link);
    grid.appendChild(div);
  });
  document.getElementById("resultCard").style.display = "block";
  updateCount();
}

async function downloadZip(mode) {
  let files = [];
  if (mode === "selected") {
    document.querySelectorAll(".result-item input:checked").forEach((cb) => {
      files.push(cb.closest(".result-item").dataset.file);
    });
    if (!files.length) {
      showAlert("Pilih minimal 1 kartu terlebih dahulu.", "error");
      return;
    }
  }
  const res = await fetch("/api/download-zip", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ job_id: currentJobId, files }),
  });
  const blob = await res.blob();
  dlBlob(blob, "kartu.zip");
}

function selectAll(checked) {
  document
    .querySelectorAll(".result-item input[type=checkbox]")
    .forEach((cb) => (cb.checked = checked));
  updateCount();
}

function updateCount() {
  const n = document.querySelectorAll(".result-item input:checked").length;
  document.getElementById("selectedCount").textContent = n + " dipilih";
}

function setProgress(pct, text) {
  document.getElementById("progressBar").style.width = pct + "%";
  document.getElementById("statusText").textContent = text;
}

function resetBtn() {
  document.getElementById("btnGenerate").disabled = false;
}

function showAlert(msg, type) {
  const el = document.getElementById("alertBox");
  el.className = "alert show alert-" + type;
  el.textContent = msg;
}

function hideAlert() {
  document.getElementById("alertBox").className = "alert";
}

function dlBlob(blob, name) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

```
---

## static/style.css
```css
*,
*::before,
*::after {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
}

:root {
  --bg: #fdf6ee;
  --surface: #fff8f0;
  --card: #ffffff;
  --primary: #c2713a;
  --primary-h: #a85e2a;
  --primary-light: #fef0e6;
  --success: #2e7d52;
  --success-h: #1f6040;
  --danger: #b91c1c;
  --border: #e8d9c8;
  --text: #2c1a0e;
  --muted: #7a5c42;
  --radius: 12px;
  --shadow: 0 2px 12px rgba(120, 80, 40, .08);
  --shadow-md: 0 4px 20px rgba(120, 80, 40, .12);
}

body {
  font-family: 'Lexend', 'Segoe UI', sans-serif;
  background: var(--bg);
  color: var(--text);
  min-height: 100vh;
  font-size: 15px;
  line-height: 1.6;
}

header {
  background: linear-gradient(135deg, #8b4a1a 0%, #c2713a 60%, #d98a50 100%);
  color: #fff;
  padding: 0 1.5rem;
  box-shadow: 0 2px 12px rgba(100, 50, 10, .25);
  position: sticky;
  top: 0;
  z-index: 100;
}

.header-inner {
  max-width: 900px;
  margin: 0 auto;
  padding: .9rem 0;
}

.header-brand {
  display: flex;
  align-items: center;
  gap: .9rem;
}

.header-icon {
  width: 2.4rem;
  height: 2.4rem;
  background: rgba(255, 255, 255, .2);
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 1.2rem;
  flex-shrink: 0;
  border: 1px solid rgba(255, 255, 255, .3);
}

header h1 {
  font-size: 1.1rem;
  font-weight: 700;
  letter-spacing: -.01em;
}

header span {
  font-size: .78rem;
  opacity: .75;
  font-weight: 400;
}

.container {
  max-width: 900px;
  margin: 2rem auto;
  padding: 0 1.25rem;
  display: flex;
  flex-direction: column;
  gap: 1rem;
  padding-bottom: 3rem;
}

.card {
  background: var(--card);
  border-radius: var(--radius);
  border: 1px solid var(--border);
  padding: 1.5rem;
  box-shadow: var(--shadow);
  transition: box-shadow .2s;
}

.card:hover {
  box-shadow: var(--shadow-md);
}

.card-header {
  display: flex;
  align-items: center;
  gap: .65rem;
  margin-bottom: 1rem;
}

.card-header h2 {
  font-size: .98rem;
  font-weight: 600;
  color: var(--text);
}

.badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.6rem;
  height: 1.6rem;
  background: var(--primary);
  color: #fff;
  border-radius: 50%;
  font-size: .72rem;
  font-weight: 700;
  flex-shrink: 0;
}

.hint {
  font-size: .82rem;
  color: var(--muted);
  margin-bottom: .9rem;
  line-height: 1.55;
}

code {
  background: var(--primary-light);
  color: var(--primary-h);
  padding: .1em .4em;
  border-radius: 4px;
  font-size: .82em;
  font-family: 'Courier New', monospace;
}

.download-row {
  display: flex;
  gap: .75rem;
  flex-wrap: wrap;
}

.drop-zone {
  border: 2px dashed var(--border);
  border-radius: 10px;
  padding: 2rem 1.5rem;
  text-align: center;
  cursor: pointer;
  transition: all .2s;
  background: var(--surface);
  user-select: none;
}

.drop-zone:hover,
.drop-zone.drag {
  border-color: var(--primary);
  background: var(--primary-light);
}

.drop-zone input {
  display: none;
}

.drop-icon {
  color: var(--muted);
  margin-bottom: .6rem;
  transition: color .2s;
}

.drop-zone:hover .drop-icon,
.drop-zone.drag .drop-icon {
  color: var(--primary);
}

.drop-label {
  font-size: .875rem;
  color: var(--muted);
  font-weight: 500;
}

.file-name {
  font-size: .8rem;
  color: var(--primary);
  margin-top: .4rem;
  font-weight: 600;
  min-height: 1.2em;
}

.actions {
  display: flex;
  gap: .6rem;
  flex-wrap: wrap;
  margin-top: .9rem;
}

.toggle-group {
  display: flex;
  gap: .75rem;
  flex-wrap: wrap;
}

.toggle-item {
  cursor: pointer;
  flex: 1;
  min-width: 140px;
}

.toggle-item input {
  display: none;
}

.toggle-label {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: .35rem;
  padding: .85rem 1rem;
  border-radius: 10px;
  border: 2px solid var(--border);
  font-size: .875rem;
  font-weight: 600;
  color: var(--muted);
  transition: all .18s;
  background: var(--surface);
}

.toggle-label small {
  font-size: .72rem;
  font-weight: 400;
  opacity: .75;
}

.toggle-icon {
  display: block;
  border: 2px solid currentColor;
  border-radius: 3px;
  opacity: .5;
}

.portrait-icon {
  width: 18px;
  height: 26px;
}

.landscape-icon {
  width: 26px;
  height: 18px;
}

.toggle-item input:checked+.toggle-label {
  border-color: var(--primary);
  background: var(--primary-light);
  color: var(--primary);
}

.toggle-item input:checked+.toggle-label .toggle-icon {
  opacity: 1;
}

.upload-grid {
  display: grid;
  grid-template-columns: 1fr 1fr 1fr;
  gap: 1rem;
}

.upload-field {
  display: flex;
  flex-direction: column;
  gap: .3rem;
}

.upload-field label {
  font-size: .82rem;
  font-weight: 600;
  color: var(--text);
}

.upload-field .hint {
  margin-bottom: .4rem;
}

.file-input-wrap input[type=file] {
  width: 100%;
  padding: .45rem .6rem;
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: .8rem;
  background: var(--surface);
  cursor: pointer;
  font-family: inherit;
  color: var(--muted);
  transition: border-color .18s;
}

.file-input-wrap input[type=file]:hover {
  border-color: var(--primary);
}

.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: .4rem;
  padding: .6rem 1.25rem;
  border-radius: 8px;
  font-size: .875rem;
  font-weight: 600;
  border: none;
  cursor: pointer;
  transition: background .15s, transform .1s, opacity .15s;
  font-family: inherit;
  text-decoration: none;
  white-space: nowrap;
}

.btn:active {
  transform: scale(.97);
}

.btn-primary {
  background: var(--primary);
  color: #fff;
}

.btn-primary:hover {
  background: var(--primary-h);
}

.btn-lg {
  padding: .75rem 2rem;
  font-size: .95rem;
}

.btn-outline {
  background: #fff;
  color: var(--primary);
  border: 1.5px solid var(--primary);
}

.btn-outline:hover {
  background: var(--primary-light);
}

.btn-success {
  background: var(--success);
  color: #fff;
}

.btn-success:hover {
  background: var(--success-h);
}

.btn-sm {
  padding: .38rem .85rem;
  font-size: .78rem;
}

.btn:disabled {
  opacity: .45;
  cursor: not-allowed;
  transform: none;
}

.progress-section {
  margin-top: 1.1rem;
}

.progress-track {
  background: var(--border);
  border-radius: 99px;
  height: 7px;
  overflow: hidden;
  margin-bottom: .5rem;
}

.progress-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--primary), #e8a060);
  border-radius: 99px;
  transition: width .4s ease;
  width: 0%;
}

.status-text {
  font-size: .8rem;
  color: var(--muted);
  font-weight: 500;
}

.alert {
  border-radius: 8px;
  font-size: .85rem;
  font-weight: 500;
  display: none;
  margin-bottom: .9rem;
  padding: .75rem 1rem;
}

.alert.show {
  display: block;
}

.alert-success {
  background: #f0fdf4;
  border: 1px solid #a7f3d0;
  color: var(--success);
}

.alert-error {
  background: #fff5f5;
  border: 1px solid #fecaca;
  color: var(--danger);
}

.result-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: .6rem;
  margin-bottom: 1rem;
  padding-bottom: .9rem;
  border-bottom: 1px solid var(--border);
}

.select-actions {
  display: flex;
  align-items: center;
  gap: .5rem;
  flex-wrap: wrap;
}

.download-actions {
  display: flex;
  gap: .5rem;
  flex-wrap: wrap;
}

.selected-count {
  font-size: .8rem;
  color: var(--muted);
  font-weight: 500;
}

#resultGrid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(148px, 1fr));
  gap: .75rem;
}

.result-item {
  border: 1px solid var(--border);
  border-radius: 10px;
  overflow: hidden;
  position: relative;
  background: var(--surface);
  transition: box-shadow .18s;
}

.result-item:hover {
  box-shadow: var(--shadow-md);
}

.result-item input[type=checkbox] {
  position: absolute;
  top: .45rem;
  left: .45rem;
  width: 1.05rem;
  height: 1.05rem;
  cursor: pointer;
  accent-color: var(--primary);
}

.result-item img {
  width: 100%;
  aspect-ratio: 638/1012;
  object-fit: cover;
  display: block;
}

.result-item.landscape img {
  aspect-ratio: 1012/638;
}

.result-label {
  font-size: .72rem;
  padding: .3rem .5rem;
  text-align: center;
  border-top: 1px solid var(--border);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--muted);
  font-weight: 500;
}

.result-item a {
  display: block;
  text-align: center;
  padding: .3rem;
  font-size: .72rem;
  color: var(--primary);
  text-decoration: none;
  border-top: 1px solid var(--border);
  font-weight: 600;
  transition: background .15s;
}

.result-item a:hover {
  background: var(--primary-light);
}

@media (max-width: 700px) {
  .container {
    padding: 0 .75rem;
    margin: 1rem auto;
  }

  .card {
    padding: 1.1rem;
  }

  .upload-grid {
    grid-template-columns: 1fr;
  }

  .toggle-group {
    flex-direction: column;
  }

  .toggle-item {
    min-width: unset;
  }

  .toggle-label {
    flex-direction: row;
    justify-content: flex-start;
    padding: .65rem .9rem;
  }

  .result-toolbar {
    flex-direction: column;
    align-items: flex-start;
  }

  #resultGrid {
    grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  }

  .btn-lg {
    width: 100%;
  }
}

@media (max-width: 480px) {
  header h1 {
    font-size: .95rem;
  }

  .download-actions {
    width: 100%;
  }

  .download-actions .btn {
    flex: 1;
  }
}

```
---

## template/data.csv
```text
uid_kartu,nama_user,username,nama_instansi,nama_kartu,hp_instansi,ig_instansi,email_instansi,web_instansi,alamat_instansi,foto_user,qr_user
KRT-0001,Nama Lengkap,username01,Nama Instansi,Kartu Pelajar,+62 812 3456 7890,@instansi,info@instansi.sch.id,https://instansi.sch.id,"Jl. Contoh No. 1, Kota",foto/username01.png,qr/username01.png

```
---
