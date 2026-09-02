package session

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	redis "github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client *redis.Client
	prefix string
}

func NewRedisStore(client *redis.Client, prefix string) *RedisStore {
	return &RedisStore{
		client: client,
		prefix: prefix,
	}
}

func (s *RedisStore) key(id string) string { return s.prefix + id }

func (s *RedisStore) Create(ctx context.Context, record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	ttl := time.Until(record.ExpireAt)
	if ttl <= 0 {
		return ErrSessionExpiry
	}
	ok, err := s.client.SetNX(ctx, s.key(record.SessionID), data, ttl).Result()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("session already exists")
	}
	return nil
}

func (s *RedisStore) Get(ctx context.Context, id string) (Record, error) {
	data, err := s.client.Get(ctx, s.key(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *RedisStore) Save(ctx context.Context, record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	ttl := time.Until(record.ExpireAt)
	if ttl <= 0 {
		return ErrSessionExpiry
	}
	ok, err := s.client.SetXX(ctx, s.key(record.SessionID), data, ttl).Result()
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

func (s *RedisStore) Delete(ctx context.Context, id string) error {
	return s.client.Del(ctx, s.key(id)).Err()
}
