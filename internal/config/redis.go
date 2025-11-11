package config

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

func NewRedisClient(cfg *Config) (*redis.Client, error) {
	var rdb *redis.Client
	
	// Check if RedisURL is a full URL (like Upstash) or just host:port
	if len(cfg.RedisURL) >= 8 && (cfg.RedisURL[:8] == "redis://" || cfg.RedisURL[:9] == "rediss://") {
		// Parse full URL (Upstash format)
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Redis URL: %v", err)
		}
		rdb = redis.NewClient(opt)
	} else {
		// Use traditional host:port format
		rdb = redis.NewClient(&redis.Options{
			Addr:     cfg.RedisURL,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %v", err)
	}

	return rdb, nil
}
