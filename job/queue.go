package job

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const QueueKey = "kartu:queue"

type QueuePayload struct {
	JobID    string `json:"job_id"`
	TmpDir   string `json:"tmp_dir"`
	Portrait bool   `json:"portrait"`
	Total    int    `json:"total"`
}

type Queue struct {
	rdb *redis.Client
}

func NewQueue(rdb *redis.Client) *Queue {
	return &Queue{rdb: rdb}
}

func (q *Queue) Push(ctx context.Context, p QueuePayload) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return q.rdb.LPush(ctx, QueueKey, data).Err()
}

func (q *Queue) Pop(ctx context.Context) (*QueuePayload, error) {
	result, err := q.rdb.BRPop(ctx, 5*time.Second, QueueKey).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p QueuePayload
	if err := json.Unmarshal([]byte(result[1]), &p); err != nil {
		return nil, err
	}
	return &p, nil
}
