package handler

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"kartu-madrasah/csvutil"
	"kartu-madrasah/job"
	"kartu-madrasah/svg"
)

func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ok, err := h.Store.CanAcceptJob(ctx)
	if err != nil {
		jsonErr(w, "redis error", 500)
		return
	}
	if !ok {
		jsonErr(w, "server sedang sibuk, coba beberapa saat lagi", 503)
		return
	}

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

	rows, _, err := csvutil.Parse(csvData)
	if err != nil {
		jsonErr(w, "gagal parse CSV: "+err.Error(), 400)
		return
	}

	jobID := job.NewJobID()
	baseDir := os.Getenv("APP_DIR")
	if baseDir == "" {
		baseDir = os.TempDir()
	}
	tmpDir, err := os.MkdirTemp(baseDir, jobID)
	if err != nil {
		jsonErr(w, "gagal membuat direktori sementara", 500)
		return
	}

	inputDir := filepath.Join(tmpDir, "input")
	os.MkdirAll(filepath.Join(inputDir, "foto"), 0755)
	os.MkdirAll(filepath.Join(inputDir, "qr"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "output"), 0755)

	os.WriteFile(filepath.Join(inputDir, "desain.svg"), svgData, 0644)
	os.WriteFile(filepath.Join(inputDir, "data.csv"), csvData, 0644)

	if lf, _, err := r.FormFile("logo_instansi"); err == nil {
		data, _ := io.ReadAll(lf)
		lf.Close()
		os.WriteFile(filepath.Join(inputDir, "logo_instansi"), data, 0644)
	}

	saveMultipleFiles(r, "foto_user", filepath.Join(inputDir, "foto"))
	saveMultipleFiles(r, "qr_user", filepath.Join(inputDir, "qr"))

	if err := h.Store.Create(ctx, jobID, len(rows), tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		jsonErr(w, "gagal membuat job", 500)
		return
	}

	payload := job.QueuePayload{
		JobID:    jobID,
		TmpDir:   tmpDir,
		Portrait: portrait,
		Total:    len(rows),
	}
	if err := h.Queue.Push(ctx, payload); err != nil {
		os.RemoveAll(tmpDir)
		jsonErr(w, "gagal mengirim ke antrian", 500)
		return
	}

	jsonOK(w, map[string]string{"job_id": jobID})
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.URL.Query().Get("job_id")
	j, err := h.Store.Get(ctx, id)
	if err != nil {
		jsonErr(w, "redis error", 500)
		return
	}
	if j == nil {
		jsonErr(w, "job tidak ditemukan", 404)
		return
	}
	jsonOK(w, map[string]interface{}{
		"status": j.Status,
		"total":  j.Total,
		"done":   j.Done,
		"failed": j.Failed,
		"error":  j.Error,
	})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.URL.Query().Get("job_id")
	j, err := h.Store.Get(ctx, id)
	if err != nil {
		jsonErr(w, "redis error", 500)
		return
	}
	if j == nil {
		jsonErr(w, "job tidak ditemukan", 404)
		return
	}
	names := make([]string, len(j.Files))
	for i, f := range j.Files {
		names[i] = filepath.Base(f)
	}
	jsonOK(w, map[string]interface{}{
		"files":  names,
		"status": j.Status,
		"total":  j.Total,
		"failed": j.Failed,
	})
}

func (h *Handler) QueueInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n, err := h.Store.QueueLen(ctx)
	if err != nil {
		jsonErr(w, "redis error", 500)
		return
	}
	jsonOK(w, map[string]interface{}{
		"queue_length": n,
		"max_queue":    job.MaxQueueLen,
		"busy":         n >= int64(job.MaxQueueLen),
	})
}

func saveMultipleFiles(r *http.Request, key, dir string) {
	if r.MultipartForm == nil {
		return
	}
	for _, fh := range r.MultipartForm.File[key] {
		f, err := fh.Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(f)
		f.Close()

		if strings.EqualFold(filepath.Ext(fh.Filename), ".zip") {
			extractZipToDir(data, dir)
			continue
		}

		os.WriteFile(filepath.Join(dir, fh.Filename), data, 0644)
	}
}

func extractZipToDir(data []byte, dir string) {
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
		content, _ := io.ReadAll(rc)
		rc.Close()
		os.WriteFile(filepath.Join(dir, name), content, 0644)
	}
}

func (h *Handler) PreviewSVG(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(32 << 20)

	svgFile, _, err := r.FormFile("svg")
	if err != nil {
		jsonErr(w, "SVG tidak ditemukan", 400)
		return
	}
	svgData, _ := io.ReadAll(svgFile)
	svgFile.Close()

	cfg, err := svg.ExtractConfig(svgData)
	if err != nil {
		jsonErr(w, "gagal parse SVG: "+err.Error(), 500)
		return
	}

	jsonOK(w, cfg)
}
