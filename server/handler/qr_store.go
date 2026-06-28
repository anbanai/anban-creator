package handler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// QR login state lifetimes.
//
// qrStateTTL is the hard cap on how long a scene's state is retained; it
// mirrors the previous in-memory 5-minute cleanup cutoff.
//
// qrExpiryNotifyDelay is how long after generation a still-pending scene is
// declared "expired" (and the WebSocket client notified). It mirrors the
// previous 2-minute expiry goroutine.
const (
	qrStateTTL          = 5 * time.Minute
	qrExpiryNotifyDelay = 2 * time.Minute
)

// QR login state machine values.
const (
	qrStatusPending = "pending"
	qrStatusScanned = "scanned"
	qrStatusExpired = "expired"
	qrStatusUsed    = "used"
)

// QRStateStore persists WeChat QR-login scene state across requests.
//
// The store makes the QR flow instance-independent: state survives a server
// restart and is shareable across instances (a prerequisite for multi-instance
// deployments; full multi-instance support additionally requires a shared
// WebSocket broadcast layer, which is a separate concern).
//
// All status transitions go through CompareAndSet so the pending→scanned→used
// lifecycle is atomic and free of check-then-act races.
type QRStateStore interface {
	// Create stores a new scene in the "pending" status with the store's TTL.
	Create(ctx context.Context, scene string) error
	// Get returns the scene's current status, or ok=false if the scene does
	// not exist or has expired.
	Get(ctx context.Context, scene string) (status string, ok bool, err error)
	// CompareAndSet transitions a scene from status `from` to `to` atomically.
	// It returns the status observed at check time and ok=true only if the
	// transition occurred. If the scene is missing, status is "" and ok is
	// false. If the scene exists but is not in status `from`, status is the
	// current status and ok is false.
	CompareAndSet(ctx context.Context, scene, from, to string) (status string, ok bool, err error)
	// Delete removes a scene's state.
	Delete(ctx context.Context, scene string) error
}

// newQRStateStore returns a Redis-backed store when a client is available, and
// falls back to an in-memory store otherwise. The fallback preserves the
// system's degraded-mode behaviour (QR login still works without Redis).
func newQRStateStore(rdb *redis.Client, logger *zerolog.Logger) QRStateStore {
	if rdb == nil {
		return newMemoryQRStateStore(qrStateTTL, nil, logger)
	}
	return newRedisQRStateStore(rdb, qrStateTTL)
}

// ---------------------------------------------------------------------------
// In-memory store (degraded-mode fallback)
// ---------------------------------------------------------------------------

type memQREntry struct {
	status    string
	createdAt time.Time
}

type memoryQRStateStore struct {
	mu     sync.RWMutex
	states map[string]*memQREntry
	ttl    time.Duration
	now    func() time.Time
	logger *zerolog.Logger
	done   chan struct{}
}

// newMemoryQRStateStore creates an in-memory store. If now is nil, time.Now is
// used. The optional now injection makes expiry deterministic in tests.
func newMemoryQRStateStore(ttl time.Duration, now func() time.Time, logger *zerolog.Logger) *memoryQRStateStore {
	if now == nil {
		now = time.Now
	}
	s := &memoryQRStateStore{
		states: make(map[string]*memQREntry),
		ttl:    ttl,
		now:    now,
		logger: logger,
		done:   make(chan struct{}),
	}
	go s.cleanup()
	return s
}

// close stops the background cleanup goroutine. Only used by tests; in
// production the store lives for the process lifetime (matching the previous
// behaviour where cleanupExpiredQRStates ran forever).
func (s *memoryQRStateStore) close() {
	select {
	case <-s.done:
		// already closed
	default:
		close(s.done)
	}
}

func (s *memoryQRStateStore) expired(e *memQREntry) bool {
	return e.createdAt.Add(s.ttl).Before(s.now())
}

func (s *memoryQRStateStore) Create(_ context.Context, scene string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[scene] = &memQREntry{status: qrStatusPending, createdAt: s.now()}
	return nil
}

func (s *memoryQRStateStore) Get(_ context.Context, scene string) (string, bool, error) {
	s.mu.RLock()
	e, ok := s.states[scene]
	s.mu.RUnlock()
	if !ok || s.expired(e) {
		return "", false, nil
	}
	return e.status, true, nil
}

func (s *memoryQRStateStore) CompareAndSet(_ context.Context, scene, from, to string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.states[scene]
	if !ok || s.expired(e) {
		if ok {
			delete(s.states, scene) // reclaim lazily-expired entry
		}
		return "", false, nil
	}
	if e.status != from {
		return e.status, false, nil
	}
	e.status = to
	return to, true, nil
}

func (s *memoryQRStateStore) Delete(_ context.Context, scene string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, scene)
	return nil
}

// cleanup periodically removes entries older than the TTL. It mirrors the
// previous cleanupExpiredQRStates sweep (every 30s, >5min cutoff).
func (s *memoryQRStateStore) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			cutoff := s.now().Add(-s.ttl)
			s.mu.Lock()
			for scene, e := range s.states {
				if e.createdAt.Before(cutoff) {
					delete(s.states, scene)
				}
			}
			s.mu.Unlock()
		}
	}
}

// ---------------------------------------------------------------------------
// Redis store
// ---------------------------------------------------------------------------

// qrCASLua atomically transitions a scene's status while preserving its TTL.
// Returns {1, newStatus} on success, {0, ""} when the scene is missing, and
// {0, currentStatus} when the scene exists but is not in the expected status.
// PTTL+PX is used instead of KEEPTTL for broader Redis/miniredis compatibility.
const qrCASLua = `
local cur = redis.call('GET', KEYS[1])
if not cur then
  return {0, ''}
end
if cur == ARGV[1] then
  local ttl = redis.call('PTTL', KEYS[1])
  if ttl and ttl > 0 then
    redis.call('SET', KEYS[1], ARGV[2], 'PX', ttl)
  else
    redis.call('SET', KEYS[1], ARGV[2])
  end
  return {1, ARGV[2]}
end
return {0, cur}
`

type redisQRStateStore struct {
	rdb *redis.Client
	ttl time.Duration
	cas *redis.Script
}

func newRedisQRStateStore(rdb *redis.Client, ttl time.Duration) *redisQRStateStore {
	return &redisQRStateStore{
		rdb: rdb,
		ttl: ttl,
		cas: redis.NewScript(qrCASLua),
	}
}

func (s *redisQRStateStore) key(scene string) string {
	return "qr:login:" + scene
}

func (s *redisQRStateStore) Create(ctx context.Context, scene string) error {
	return s.rdb.Set(ctx, s.key(scene), qrStatusPending, s.ttl).Err()
}

func (s *redisQRStateStore) Get(ctx context.Context, scene string) (string, bool, error) {
	status, err := s.rdb.Get(ctx, s.key(scene)).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return status, true, nil
}

func (s *redisQRStateStore) CompareAndSet(ctx context.Context, scene, from, to string) (string, bool, error) {
	res, err := s.cas.Run(ctx, s.rdb, []string{s.key(scene)}, from, to).Result()
	if err != nil {
		return "", false, err
	}
	arr, ok := res.([]any)
	if !ok || len(arr) < 2 {
		return "", false, fmt.Errorf("qr store: unexpected CAS result type %T", res)
	}
	transitioned, _ := arr[0].(int64)
	status, _ := arr[1].(string)
	return status, transitioned == 1, nil
}

func (s *redisQRStateStore) Delete(ctx context.Context, scene string) error {
	return s.rdb.Del(ctx, s.key(scene)).Err()
}
