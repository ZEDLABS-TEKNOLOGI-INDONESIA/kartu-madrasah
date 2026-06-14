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

//go:embed static/index.html static/style.css static/script.js static/favicon.ico template/data.csv template/desain.svg
var staticFiles embed.FS

const (
	ktpPortraitW  = 638
	ktpPortraitH  = 1012
	ktpLandscapeW = 1012
	ktpLandscapeH = 638
	dpi           = 300
	maxWorkers    = 4
	jobTTL        = 2 * time.Hour
)

var globalSem = make(chan struct{}, maxWorkers)

type Job struct {
	ID        string
	Status    string
	Files     []string
	Error     string
	Total     int
	Done      int
	CreatedAt time.Time
	mu        sync.Mutex
}

type JobStore struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func (s *JobStore) New(id string) *Job {
	j := &Job{ID: id, Status: "pending", CreatedAt: time.Now()}
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

func (s *JobStore) cleanup() {
	for {
		time.Sleep(30 * time.Minute)
		s.mu.Lock()
		for id, j := range s.jobs {
			j.mu.Lock()
			expired := time.Since(j.CreatedAt) > jobTTL && (j.Status == "done" || j.Status == "error")
			files := j.Files
			j.mu.Unlock()
			if expired {
				for _, f := range files {
					os.Remove(f)
				}
				if len(files) > 0 {
					os.RemoveAll(filepath.Dir(files[0]))
				}
				delete(s.jobs, id)
			}
		}
		s.mu.Unlock()
	}
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

func runInkscape(svgPath, pngPath string, portrait bool) error {
	globalSem <- struct{}{}
	defer func() { <-globalSem }()

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
	data, err := staticFiles.ReadFile("template/data.csv")
	if err != nil {
		jsonErr(w, "template CSV tidak ditemukan", 500)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="template_data.csv"`)
	w.Header().Set("Content-Type", "text/csv")
	w.Write(data)
}

func handleTemplateSVG(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("template/desain.svg")
	if err != nil {
		jsonErr(w, "template SVG tidak ditemukan", 500)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="desain.svg"`)
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write(data)
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
	job.Total = len(rows)
	tmpDir, _ := os.MkdirTemp("", jobID)

	go func() {
		job.mu.Lock()
		job.Status = "processing"
		job.mu.Unlock()

		type prepared struct {
			svgPath string
			pngPath string
		}

		var preparedList []prepared
		for _, row := range rows {
			uid := row["username"]
			if uid == "" {
				uid = row["uid_kartu"]
			}
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
			if err := os.WriteFile(svgPath, injected, 0644); err != nil {
				continue
			}
			preparedList = append(preparedList, prepared{svgPath, pngPath})
		}

		var wg sync.WaitGroup
		for _, p := range preparedList {
			wg.Add(1)
			go func(p prepared) {
				defer wg.Done()
				runInkscape(p.svgPath, p.pngPath, portrait)
				os.Remove(p.svgPath)
				job.mu.Lock()
				job.Done++
				job.mu.Unlock()
			}(p)
		}
		wg.Wait()

		var outFiles []string
		for _, p := range preparedList {
			if _, err := os.Stat(p.pngPath); err == nil {
				outFiles = append(outFiles, p.pngPath)
			}
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
		"total":  job.Total,
		"done":   job.Done,
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

	go store.cleanup()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/template-csv", handleTemplateCSV)
	mux.HandleFunc("/api/template-svg", handleTemplateSVG)
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
