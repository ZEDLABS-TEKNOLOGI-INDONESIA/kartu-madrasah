package job

import (
	"context"
	"log"
	"os"
	"time"
)

func (s *Store) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sweepExpiredDirs(ctx)
			}
		}
	}()
}

func (s *Store) sweepExpiredDirs(ctx context.Context) {
	var cursor uint64
	for {
		keys, next, err := s.rdb.Scan(ctx, cursor, "kartu:job:*", 100).Result()
		if err != nil {
			log.Printf("[cleanup] scan error: %v", err)
			return
		}
		for _, key := range keys {
			if len(key) > 7 && key[len(key)-7:] == ":errors" {
				continue
			}
			ttl, err := s.rdb.TTL(ctx, key).Result()
			if err != nil {
				continue
			}
			if ttl >= 0 && ttl < 5*time.Minute {
				tmpDir, _ := s.rdb.HGet(ctx, key, "tmp_dir").Result()
				if tmpDir != "" {
					if err := os.RemoveAll(tmpDir); err == nil {
						log.Printf("[cleanup] removed: %s", tmpDir)
					}
				}
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
}
