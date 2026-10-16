package ratelimit

import (
	"strconv"
	"testing"
	"testing/synctest"
	"time"
)

func TestStore_SweepRemovesRecoveredKeys(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newStore(time.Second, 10*time.Second) // T = 1s, capacity 10s
		s.allow("a", time.Now())
		s.allow("b", time.Now())
		if n := s.len(); n != 2 {
			t.Fatalf("len = %d, want 2", n)
		}
		time.Sleep(11 * time.Second)
		// Touch every shard so that each one sweeps.
		for i := 0; (s.has("a") || s.has("b")) && i < 100000; i++ {
			s.allow("probe-"+strconv.Itoa(i), time.Now())
		}
		if s.has("a") || s.has("b") {
			t.Error("recovered keys were not swept")
		}
	})
}

func TestRateLimit_ReclaimsIdleKeys(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const clients = 1_000_000
		s := newStore(time.Second, 10*time.Second)
		now := time.Now()
		for i := range clients {
			s.allow("client-"+strconv.Itoa(i), now)
		}
		if n := s.len(); n != clients {
			t.Fatalf("len = %d, want %d", n, clients)
		}
		time.Sleep(2*10*time.Second + time.Second)

		// Follow-up traffic that reaches every shard.
		seen := map[uint64]bool{}
		probes := 0
		for i := 0; len(seen) < shardCount; i++ {
			key := "probe-" + strconv.Itoa(i)
			seen[s.shardIndex(key)] = true
			s.allow(key, time.Now())
			probes++
		}
		if n := s.len(); n != probes {
			t.Errorf("retained %d keys after the idle period, want only the %d follow-up keys", n, probes)
		}
	})
}
