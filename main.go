package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}()

	fmt.Printf("Server berjalan di http://localhost:%s\n", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Server berhenti.")
}
