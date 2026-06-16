package handler

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func (h *Handler) DownloadOne(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("job_id")
	name := filepath.Base(r.URL.Query().Get("file"))

	j := h.Store.Get(id)
	if j == nil {
		jsonErr(w, "job tidak ditemukan", 404)
		return
	}

	j.Mu.Lock()
	files := j.Files
	j.Mu.Unlock()

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

func (h *Handler) DownloadZip(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JobID string   `json:"job_id"`
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "request tidak valid", 400)
		return
	}

	j := h.Store.Get(req.JobID)
	if j == nil {
		jsonErr(w, "job tidak ditemukan", 404)
		return
	}

	j.Mu.Lock()
	allFiles := j.Files
	j.Mu.Unlock()

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
		file, err := os.Open(f)
		if err != nil {
			continue
		}
		io.Copy(fw, file)
		file.Close()
	}
}
