package ratelimit

import (
	"hash/maphash"
	"sync"
	"time"
)

const shardCount = 64

type shard struct {
	mu        sync.Mutex
	tats      map[string]time.Time
	nextSweep time.Time
}

// store keeps the TAT of every key in shards. Keys whose allowance has fully
// recovered are swept lazily by the requests that reach their shard: no
// goroutine is started.
type store struct {
	seed     maphash.Seed
	interval time.Duration
	capacity time.Duration
	shards   [shardCount]shard
}

func newStore(interval, capacity time.Duration) *store {
	s := &store{seed: maphash.MakeSeed(), interval: interval, capacity: capacity}
	for i := range s.shards {
		s.shards[i].tats = map[string]time.Time{}
	}
	return s
}

func (s *store) shardIndex(key string) uint64 {
	return maphash.String(s.seed, key) % shardCount
}

// allow records a request for key at now and reports whether it is allowed
// and, if not, how long until a request would be.
func (s *store) allow(key string, now time.Time) (bool, time.Duration) {
	sh := &s.shards[s.shardIndex(key)]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if !now.Before(sh.nextSweep) {
		sh.sweep(now)
		sh.nextSweep = now.Add(s.capacity)
	}
	tat, allowed, retry := decide(sh.tats[key], now, s.interval, s.capacity)
	if allowed {
		sh.tats[key] = tat
	}
	return allowed, retry
}

// sweep deletes the keys whose allowance has fully recovered (TAT <= now).
// When most keys are gone the map is reallocated, because Go maps never
// shrink.
func (sh *shard) sweep(now time.Time) {
	before := len(sh.tats)
	for k, tat := range sh.tats {
		if !tat.After(now) {
			delete(sh.tats, k)
		}
	}
	if removed := before - len(sh.tats); removed > 0 && removed >= len(sh.tats) {
		fresh := make(map[string]time.Time, len(sh.tats))
		for k, tat := range sh.tats {
			fresh[k] = tat
		}
		sh.tats = fresh
	}
}

func (s *store) len() int {
	n := 0
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		n += len(sh.tats)
		sh.mu.Unlock()
	}
	return n
}

func (s *store) has(key string) bool {
	sh := &s.shards[s.shardIndex(key)]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	_, ok := sh.tats[key]
	return ok
}
