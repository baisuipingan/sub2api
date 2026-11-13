//go:build unit

package repository

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestDesktopAuthCodeStoreAtomicExpiryAndFailure(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewDesktopAuthCodeStore(rdb)
	ctx := context.Background()
	hash := strings.Repeat("a", 64)
	entry := &service.DesktopAuthCode{UserID: 7, RedirectURI: "http://127.0.0.1:49152/callback", ExpiresAt: time.Now().Add(time.Minute)}
	require.NoError(t, store.StoreDesktopAuthCode(ctx, hash, entry, time.Minute))
	require.Error(t, store.StoreDesktopAuthCode(ctx, hash, entry, time.Minute))
	var count atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := store.ConsumeDesktopAuthCode(ctx, hash)
			if err == nil {
				if got.UserID != 7 {
					t.Error("unexpected user")
				}
				count.Add(1)
			} else if !errors.Is(err, service.ErrDesktopAuthCodeNotFound) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, count.Load())
	require.NoError(t, store.StoreDesktopAuthCode(ctx, hash, entry, time.Minute))
	mr.FastForward(2 * time.Minute)
	_, err := store.ConsumeDesktopAuthCode(ctx, hash)
	require.ErrorIs(t, err, service.ErrDesktopAuthCodeNotFound)
	mr.Close()
	_, err = store.ConsumeDesktopAuthCode(ctx, hash)
	require.Error(t, err)
	require.NotErrorIs(t, err, service.ErrDesktopAuthCodeNotFound)
}
