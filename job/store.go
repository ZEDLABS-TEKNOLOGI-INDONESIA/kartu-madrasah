package job

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	JobTTL      = 2 * time.Hour
	MaxQueueLen = 1000
)

type JobData struct {
	ID     string   `json:"id"`
	Status string   `json:"status"`
	Files  []string `json:"files"`
	Error  string   `json:"error"`
	Total  int      `json:"total"`
	Done   int      `json:"done"`
	Failed int      `json:"failed"`
	TmpDir string   `json:"tmp_dir"`
}

type Store struct {
	rdb *redis.Client
}

func NewStore(rdb *redis.Client) *Store {
	return &Store{rdb: rdb}
}

func NewJobID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "job_" + hex.EncodeToString(b)
}

func jobKey(id string) string    { return "kartu:job:" + id }
func jobErrKey(id string) string { return "kartu:job:" + id + ":errors" }

func (s *Store) Create(ctx context.Context, id string, total int, tmpDir string) error {
	key := jobKey(id)
	pipe := s.rdb.Pipeline()
	pipe.HSet(ctx, key,
		"id", id,
		"status", "pending",
		"total", total,
		"done", 0,
		"failed", 0,
		"tmp_dir", tmpDir,
		"files", "[]",
	)
	pipe.Expire(ctx, key, JobTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Store) Get(ctx context.Context, id string) (*JobData, error) {
	vals, err := s.rdb.HGetAll(ctx, jobKey(id)).Result()
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 {
		return nil, nil
	}
	j := &JobData{
		ID:     vals["id"],
		Status: vals["status"],
		TmpDir: vals["tmp_dir"],
	}
	j.Total, _ = strconv.Atoi(vals["total"])
	j.Done, _ = strconv.Atoi(vals["done"])
	j.Failed, _ = strconv.Atoi(vals["failed"])
	if v := vals["files"]; v != "" {
		json.Unmarshal([]byte(v), &j.Files)
	}
	errs, _ := s.rdb.LRange(ctx, jobErrKey(id), 0, -1).Result()
	for _, e := range errs {
		j.Error += e + "\n"
	}
	return j, nil
}

func (s *Store) SetStatus(ctx context.Context, id, status string) error {
	return s.rdb.HSet(ctx, jobKey(id), "status", status).Err()
}

func (s *Store) IncrDone(ctx context.Context, id string) error {
	return s.rdb.HIncrBy(ctx, jobKey(id), "done", 1).Err()
}

func (s *Store) IncrFailed(ctx context.Context, id, errMsg string) error {
	pipe := s.rdb.Pipeline()
	pipe.HIncrBy(ctx, jobKey(id), "failed", 1)
	pipe.HIncrBy(ctx, jobKey(id), "done", 1)
	pipe.RPush(ctx, jobErrKey(id), errMsg)
	pipe.Expire(ctx, jobErrKey(id), JobTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Store) Finish(ctx context.Context, id, status string, total int, files []string) error {
	filesJSON, _ := json.Marshal(files)
	pipe := s.rdb.Pipeline()
	pipe.HSet(ctx, jobKey(id),
		"status", status,
		"files", string(filesJSON),
		"done", total,
	)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Store) CanAcceptJob(ctx context.Context) (bool, error) {
	n, err := s.rdb.LLen(ctx, QueueKey).Result()
	if err != nil {
		return false, err
	}
	return n < MaxQueueLen, nil
}

func (s *Store) QueueLen(ctx context.Context) (int64, error) {
	return s.rdb.LLen(ctx, QueueKey).Result()
}
