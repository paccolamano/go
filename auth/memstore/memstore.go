// Package memstore is an in-memory auth.Store for tests and single-instance
// deployments. Its state is lost when the process exits.
package memstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/paccolamano/go/auth"
)

// sweepInterval is the minimum time between two sweeps of expired entries.
const sweepInterval = time.Minute

type entry struct {
	value     []byte
	counter   int64
	isCounter bool
	expires   time.Time
}

// Store is an in-memory auth.Store, safe for concurrent use. Expired entries
// are never returned; their memory is reclaimed when they are touched, and by a
// sweep that runs inside a write at most once per minute. Store starts no
// goroutine and needs no closing.
type Store struct {
	mu        sync.Mutex
	m         map[string]entry
	lastSweep time.Time
}

// New returns an empty Store.
func New() *Store {
	return &Store{m: make(map[string]entry), lastSweep: time.Now()}
}

var errBadTTL = errors.New("memstore: ttl must be greater than 0")

// Get returns a copy of the value stored under key, or an error matching
// auth.ErrNotFound when the key is absent or expired. A counter is returned in
// decimal.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("memstore: get: %w", err)
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok {
		return nil, auth.ErrNotFound
	}
	if !now.Before(e.expires) {
		delete(s.m, key)
		return nil, auth.ErrNotFound
	}
	if e.isCounter {
		return []byte(strconv.FormatInt(e.counter, 10)), nil
	}
	return append([]byte(nil), e.value...), nil
}

// Set stores a copy of value under key for ttl, replacing any previous value.
func (s *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("memstore: set: %w", err)
	}
	if ttl <= 0 {
		return errBadTTL
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = entry{value: append([]byte(nil), value...), expires: now.Add(ttl)}
	s.sweep(now)
	return nil
}

// Delete removes key. Deleting an absent key is not an error.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("memstore: delete: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

// Incr atomically adds 1 to the counter under key and returns the new value.
// An absent or expired counter is created at 1 with expiry now + ttl; later
// increments keep that expiry.
func (s *Store) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("memstore: incr: %w", err)
	}
	if ttl <= 0 {
		return 0, errBadTTL
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	switch {
	case !ok || !now.Before(e.expires):
		e = entry{isCounter: true, expires: now.Add(ttl)}
	case !e.isCounter:
		return 0, errors.New("memstore: incr: key holds a value, not a counter")
	}
	e.counter++
	s.m[key] = e
	s.sweep(now)
	return e.counter, nil
}

// sweep removes every expired entry when sweepInterval has passed since the
// previous sweep. s.mu must be held.
func (s *Store) sweep(now time.Time) {
	if now.Sub(s.lastSweep) < sweepInterval {
		return
	}
	for k, e := range s.m {
		if !now.Before(e.expires) {
			delete(s.m, k)
		}
	}
	s.lastSweep = now
}
