package handler

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (h *Handler) DownloadOne(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.URL.Query().Get("job_id")
	name := filepath.Base(r.URL.Query().Get("file"))

	j, err := h.Store.Get(ctx, id)
	if err != nil {
		jsonErr(w, "redis error", 500)
		return
	}
	if j == nil {
		jsonErr(w, "job tidak ditemukan", 404)
		return
	}

	for _, f := range j.Files {
		if filepath.Base(f) != name {
			continue
		}
		absF, err := filepath.Abs(f)
		if err != nil {
			jsonErr(w, "path tidak valid", 400)
			return
		}
		absBase, err := filepath.Abs(j.TmpDir)
		if err != nil {
			jsonErr(w, "path tidak valid", 400)
			return
		}
		if !strings.HasPrefix(absF, absBase+string(filepath.Separator)) {
			jsonErr(w, "akses ditolak", 403)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Header().Set("Content-Type", "image/png")
		http.ServeFile(w, r, absF)
		return
	}

	jsonErr(w, "file tidak ditemukan", 404)
}

func (h *Handler) DownloadZip(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		JobID string   `json:"job_id"`
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "request tidak valid", 400)
		return
	}

	j, err := h.Store.Get(ctx, req.JobID)
	if err != nil {
		jsonErr(w, "redis error", 500)
		return
	}
	if j == nil {
		jsonErr(w, "job tidak ditemukan", 404)
		return
	}

	selected := j.Files
	if len(req.Files) > 0 {
		sel := make(map[string]bool, len(req.Files))
		for _, f := range req.Files {
			sel[filepath.Base(f)] = true
		}
		selected = nil
		for _, f := range j.Files {
			if sel[filepath.Base(f)] {
				selected = append(selected, f)
			}
		}
	}

	absBase, err := filepath.Abs(j.TmpDir)
	if err != nil {
		jsonErr(w, "path tidak valid", 400)
		return
	}

	w.Header().Set("Content-Disposition", `attachment; filename="kartu.zip"`)
	w.Header().Set("Content-Type", "application/zip")

	zw := zip.NewWriter(w)
	defer zw.Close()

	for _, f := range selected {
		absF, err := filepath.Abs(f)
		if err != nil || !strings.HasPrefix(absF, absBase+string(filepath.Separator)) {
			continue
		}
		fw, err := zw.Create(filepath.Base(f))
		if err != nil {
			continue
		}
		file, err := os.Open(absF)
		if err != nil {
			continue
		}
		if _, err := io.Copy(fw, file); err != nil {
			file.Close()
			continue
		}
		file.Close()
	}
}
