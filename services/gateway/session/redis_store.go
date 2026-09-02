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

var compareAndSwapScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current or current ~= ARGV[1] then
    return 0
end
redis.call("SET", KEYS[1], ARGV[2], "PX", ARGV[3])
return 1
`)

var claimAccountScript = redis.NewScript(`
local previous = redis.call("GET", KEYS[1])
redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
return previous or ""
`)

var releaseAccountScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then return 1 end
local decoded = cjson.decode(current)
if decoded.connection_id == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
`)

func NewRedisStore(client *redis.Client, prefix string) *RedisStore {
	return &RedisStore{
		client: client,
		prefix: prefix,
	}
}

func (s *RedisStore) key(id string) string { return s.prefix + id }

func (s *RedisStore) accountKey(accountID string) string { return s.prefix + "account:" + accountID }

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

func (s *RedisStore) ClaimAccount(ctx context.Context, accountID string, record Record) (Record, bool, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return Record{}, false, err
	}
	ttl := time.Until(record.ExpireAt)
	if ttl <= 0 {
		return Record{}, false, ErrSessionExpiry
	}
	previousData, err := claimAccountScript.Run(ctx, s.client, []string{s.accountKey(accountID)}, data, ttl.Milliseconds()).Text()
	if err != nil {
		return Record{}, false, err
	}
	if previousData == "" {
		return Record{}, false, nil
	}
	var previous Record
	if err := json.Unmarshal([]byte(previousData), &previous); err != nil {
		return Record{}, false, err
	}
	return previous, true, nil
}

func (s *RedisStore) ReleaseAccount(ctx context.Context, accountID, connectionID string) error {
	_, err := releaseAccountScript.Run(ctx, s.client, []string{s.accountKey(accountID)}, connectionID).Result()
	return err
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

func (s *RedisStore) CompareAndSwap(ctx context.Context, expected, updated Record) (bool, error) {
	expectedData, err := json.Marshal(expected)
	if err != nil {
		return false, err
	}
	updatedData, err := json.Marshal(updated)
	if err != nil {
		return false, err
	}
	ttl := time.Until(updated.ExpireAt)
	if ttl <= 0 {
		return false, ErrSessionExpiry
	}
	result, err := compareAndSwapScript.Run(ctx, s.client, []string{s.key(updated.SessionID)}, expectedData, updatedData, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (s *RedisStore) Delete(ctx context.Context, id string) error {
	return s.client.Del(ctx, s.key(id)).Err()
}
