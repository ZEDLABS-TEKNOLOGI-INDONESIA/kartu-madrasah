package job

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

const JobTTL = 2 * time.Hour

type Job struct {
	ID        string
	Status    string
	Files     []string
	Error     string
	Total     int
	Done      int
	CreatedAt time.Time
	Mu        sync.Mutex
}

type Store struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func NewStore() *Store {
	return &Store{jobs: make(map[string]*Job)}
}

func (s *Store) New(id string) *Job {
	j := &Job{ID: id, Status: "pending", CreatedAt: time.Now()}
	s.mu.Lock()
	s.jobs[id] = j
	s.mu.Unlock()
	return j
}

func (s *Store) Get(id string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

func (s *Store) Cleanup() {
	for {
		time.Sleep(30 * time.Minute)
		s.mu.Lock()
		for id, j := range s.jobs {
			j.Mu.Lock()
			expired := time.Since(j.CreatedAt) > JobTTL &&
				(j.Status == "done" || j.Status == "error")
			files := j.Files
			j.Mu.Unlock()
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
