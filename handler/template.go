package handler

import (
	"io"
	"net/http"
	"path/filepath"

	"kartu-madrasah/svg"

	"gopkg.in/yaml.v3"
)

func (h *Handler) Static(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "static/index.html"
	} else {
		path = "static" + path
	}

	data, err := h.StaticFiles.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer data.Close()

	content, err := io.ReadAll(data)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	ext := filepath.Ext(path)
	switch ext {
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
	case ".ico":
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "public, max-age=604800")
	default:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Write(content)
}

func (h *Handler) TemplateCSV(w http.ResponseWriter, r *http.Request) {
	data, err := h.StaticFiles.Open("template/data.csv")
	if err != nil {
		jsonErr(w, "template CSV tidak ditemukan", 500)
		return
	}
	defer data.Close()

	content, _ := io.ReadAll(data)
	w.Header().Set("Content-Disposition", `attachment; filename="template_data.csv"`)
	w.Header().Set("Content-Type", "text/csv")
	w.Write(content)
}

func (h *Handler) TemplateSVG(w http.ResponseWriter, r *http.Request) {
	data, err := h.StaticFiles.Open("template/desain.svg")
	if err != nil {
		jsonErr(w, "template SVG tidak ditemukan", 500)
		return
	}
	defer data.Close()

	content, _ := io.ReadAll(data)
	w.Header().Set("Content-Disposition", `attachment; filename="desain.svg"`)
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write(content)
}

func (h *Handler) ExtractConfig(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(32 << 20)

	f, _, err := r.FormFile("svg")
	if err != nil {
		jsonErr(w, "file SVG tidak ditemukan: "+err.Error(), 400)
		return
	}
	defer f.Close()

	svgData, _ := io.ReadAll(f)

	cfg, err := svg.ExtractConfig(svgData)
	if err != nil {
		jsonErr(w, "gagal parse SVG: "+err.Error(), 500)
		return
	}

	yamlOut, _ := yaml.Marshal(cfg)
	w.Header().Set("Content-Disposition", `attachment; filename="config.yaml"`)
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Write(yamlOut)
}
