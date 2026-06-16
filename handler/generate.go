package handler

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kartu-madrasah/csvutil"
	"kartu-madrasah/job"
	"kartu-madrasah/svg"
)

func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
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

	globalImages := map[string][]byte{}
	if lf, _, err := r.FormFile("logo_instansi"); err == nil {
		globalImages["logo_instansi"], _ = io.ReadAll(lf)
		lf.Close()
	}

	fotoMap := readMultipleFiles(r, "foto_user")
	qrMap := readMultipleFiles(r, "qr_user")

	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	j := h.Store.New(jobID)
	j.Total = len(rows)

	tmpDir, _ := os.MkdirTemp("", jobID)

	go func() {
		j.Mu.Lock()
		j.Status = "processing"
		j.Mu.Unlock()

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

			injected, err := svg.Inject(svgData, row, imgs)
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
				svg.RunInkscape(p.svgPath, p.pngPath, portrait)
				os.Remove(p.svgPath)
				j.Mu.Lock()
				j.Done++
				j.Mu.Unlock()
			}(p)
		}
		wg.Wait()

		var outFiles []string
		for _, p := range preparedList {
			if _, err := os.Stat(p.pngPath); err == nil {
				outFiles = append(outFiles, p.pngPath)
			}
		}

		j.Mu.Lock()
		j.Status = "done"
		j.Files = outFiles
		j.Mu.Unlock()
	}()

	jsonOK(w, map[string]string{"job_id": jobID})
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("job_id")
	j := h.Store.Get(id)
	if j == nil {
		jsonErr(w, "job tidak ditemukan", 404)
		return
	}
	j.Mu.Lock()
	defer j.Mu.Unlock()
	jsonOK(w, map[string]interface{}{
		"status": j.Status,
		"total":  j.Total,
		"done":   j.Done,
		"error":  j.Error,
	})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("job_id")
	j := h.Store.Get(id)
	if j == nil {
		jsonErr(w, "job tidak ditemukan", 404)
		return
	}
	j.Mu.Lock()
	defer j.Mu.Unlock()
	names := make([]string, len(j.Files))
	for i, f := range j.Files {
		names[i] = filepath.Base(f)
	}
	jsonOK(w, map[string]interface{}{
		"files":  names,
		"status": j.Status,
	})
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
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp"} {
		if d, ok := m[username+ext]; ok {
			return d
		}
	}
	if d, ok := m[username]; ok {
		return d
	}
	return nil
}

// helper untuk job store — dipakai juga di handler lain
func getJob(h *Handler, w http.ResponseWriter, r *http.Request) *job.Job {
	id := r.URL.Query().Get("job_id")
	j := h.Store.Get(id)
	if j == nil {
		jsonErr(w, "job tidak ditemukan", 404)
	}
	return j
}
