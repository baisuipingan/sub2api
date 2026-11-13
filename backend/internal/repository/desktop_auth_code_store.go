package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type desktopAuthCodeStore struct{ rdb *redis.Client }

func NewDesktopAuthCodeStore(rdb *redis.Client) service.DesktopAuthCodeStore {
	return &desktopAuthCodeStore{rdb: rdb}
}

func (s *desktopAuthCodeStore) StoreDesktopAuthCode(ctx context.Context, hash string, entry *service.DesktopAuthCode, ttl time.Duration) error {
	if s.rdb == nil || len(hash) != 64 || entry == nil || ttl <= 0 {
		return errors.New("desktop authorization store unavailable")
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	created, err := s.rdb.SetNX(ctx, "desktop:auth:code:"+hash, data, ttl).Result()
	if err != nil {
		return err
	}
	if !created {
		return errors.New("desktop authorization code collision")
	}
	return nil
}

func (s *desktopAuthCodeStore) ConsumeDesktopAuthCode(ctx context.Context, hash string) (*service.DesktopAuthCode, error) {
	if s.rdb == nil || len(hash) != 64 {
		return nil, errors.New("desktop authorization store unavailable")
	}
	// Redis GETDEL is atomic across replicas of the application; never GET then DEL.
	data, err := s.rdb.GetDel(ctx, "desktop:auth:code:"+hash).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, service.ErrDesktopAuthCodeNotFound
	}
	if err != nil {
		return nil, err
	}
	var entry service.DesktopAuthCode
	if err = json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}
