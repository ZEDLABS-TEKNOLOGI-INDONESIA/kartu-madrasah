package handler

import (
	"encoding/json"
	"io/fs"
	"net/http"

	"kartu-madrasah/job"
)

type Handler struct {
	Store       *job.Store
	Queue       *job.Queue
	StaticFiles fs.FS
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
