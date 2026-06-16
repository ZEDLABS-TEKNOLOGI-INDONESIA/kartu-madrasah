package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"

	"kartu-madrasah/handler"
	"kartu-madrasah/job"
)

//go:embed static/index.html static/style.css static/script.js static/favicon.ico template/data.csv template/desain.svg
var embeddedFiles embed.FS

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8090"
	}

	store := job.NewStore()
	go store.Cleanup()

	staticFS, err := fs.Sub(embeddedFiles, ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gagal load static files: %v\n", err)
		os.Exit(1)
	}

	h := &handler.Handler{
		Store:       store,
		StaticFiles: staticFS,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/template-csv", h.TemplateCSV)
	mux.HandleFunc("/api/template-svg", h.TemplateSVG)
	mux.HandleFunc("/api/extract-config", h.ExtractConfig)
	mux.HandleFunc("/api/generate", h.Generate)
	mux.HandleFunc("/api/status", h.Status)
	mux.HandleFunc("/api/list", h.List)
	mux.HandleFunc("/api/download", h.DownloadOne)
	mux.HandleFunc("/api/download-zip", h.DownloadZip)
	mux.HandleFunc("/", h.Static)

	fmt.Printf("Server berjalan di http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
