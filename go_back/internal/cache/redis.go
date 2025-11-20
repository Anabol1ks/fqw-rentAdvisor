package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisClient обертка над Redis клиентом
type RedisClient struct {
	client *redis.Client
	prefix string
}

// NewRedisClient создает новый Redis клиент
func NewRedisClient(addr, password string, db int, prefix string) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	// Проверяем соединение
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &RedisClient{
		client: client,
		prefix: prefix,
	}, nil
}

// Get получает значение из кэша
func (r *RedisClient) Get(ctx context.Context, key string) (string, error) {
	fullKey := r.prefix + key
	val, err := r.client.Get(ctx, fullKey).Result()
	if err == redis.Nil {
		return "", nil // Ключ не найден
	}
	if err != nil {
		return "", fmt.Errorf("redis get error: %w", err)
	}
	return val, nil
}

// Set сохраняет значение в кэш с TTL
func (r *RedisClient) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	fullKey := r.prefix + key

	var data string
	switch v := value.(type) {
	case string:
		data = v
	default:
		jsonData, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %w", err)
		}
		data = string(jsonData)
	}

	if err := r.client.Set(ctx, fullKey, data, ttl).Err(); err != nil {
		return fmt.Errorf("redis set error: %w", err)
	}
	return nil
}

// GetJSON получает и десериализует JSON из кэша
func (r *RedisClient) GetJSON(ctx context.Context, key string, dest interface{}) (bool, error) {
	val, err := r.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if val == "" {
		return false, nil // Не найдено
	}

	if err := json.Unmarshal([]byte(val), dest); err != nil {
		return false, fmt.Errorf("failed to unmarshal cached data: %w", err)
	}
	return true, nil
}

// Delete удаляет ключ из кэша
func (r *RedisClient) Delete(ctx context.Context, key string) error {
	fullKey := r.prefix + key
	return r.client.Del(ctx, fullKey).Err()
}

// Close закрывает соединение с Redis
func (r *RedisClient) Close() error {
	return r.client.Close()
}

// Ping проверяет соединение с Redis
func (r *RedisClient) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}
