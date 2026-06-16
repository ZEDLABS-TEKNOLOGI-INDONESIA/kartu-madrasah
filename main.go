package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"kartu-madrasah/handler"
	"kartu-madrasah/job"
	"kartu-madrasah/worker"

	"github.com/redis/go-redis/v9"
)

//go:embed static/index.html static/style.css static/script.js static/favicon.ico template/data.csv template/desain.svg
var embeddedFiles embed.FS

func cleanupOrphanedJobs(baseDir string) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "job_") {
			os.RemoveAll(filepath.Join(baseDir, e.Name()))
		}
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8090"
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "redis URL tidak valid: %v\n", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(opt)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "gagal koneksi ke Redis: %v\n", err)
		os.Exit(1)
	}

	appDir := os.Getenv("APP_DIR")
	if appDir == "" {
		appDir = os.TempDir()
	}
	cleanupOrphanedJobs(appDir)

	store := job.NewStore(rdb)
	queue := job.NewQueue(rdb)

	store.StartCleanup(ctx)

	w := worker.New(store, queue)
	w.Start(ctx)

	staticFS, err := fs.Sub(embeddedFiles, ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gagal load static files: %v\n", err)
		os.Exit(1)
	}

	h := &handler.Handler{
		Store:       store,
		Queue:       queue,
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
	mux.HandleFunc("/api/queue-info", h.QueueInfo)
	mux.HandleFunc("/", h.Static)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

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
