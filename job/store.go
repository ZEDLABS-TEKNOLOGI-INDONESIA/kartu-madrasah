package job

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	JobTTL        = 2 * time.Hour
	MaxActiveJobs = 10
)

type Job struct {
	ID        string
	Status    string
	Files     []string
	Error     string
	Total     int
	Done      int
	Failed    int
	CreatedAt time.Time
	Mu        sync.Mutex
}

type Store struct {
	mu         sync.Mutex
	jobs       map[string]*Job
	activeJobs int32
}

func NewStore() *Store {
	return &Store{jobs: make(map[string]*Job)}
}

func NewJobID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "job_" + hex.EncodeToString(b)
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

func (s *Store) CanAcceptJob() bool {
	return atomic.LoadInt32(&s.activeJobs) < MaxActiveJobs
}

func (s *Store) IncrActive() {
	atomic.AddInt32(&s.activeJobs, 1)
}

func (s *Store) DecrActive() {
	atomic.AddInt32(&s.activeJobs, -1)
}

func (s *Store) Cleanup() {
	type toDelete struct {
		files []string
	}

	for {
		time.Sleep(30 * time.Minute)

		var expired []toDelete

		s.mu.Lock()
		for id, j := range s.jobs {
			j.Mu.Lock()
			isExpired := time.Since(j.CreatedAt) > JobTTL &&
				(j.Status == "done" || j.Status == "error")
			files := j.Files
			j.Mu.Unlock()
			if isExpired {
				expired = append(expired, toDelete{files})
				delete(s.jobs, id)
			}
		}
		s.mu.Unlock()

		for _, e := range expired {
			for _, f := range e.files {
				os.Remove(f)
			}
			if len(e.files) > 0 {
				os.RemoveAll(filepath.Dir(e.files[0]))
			}
		}
	}
}
