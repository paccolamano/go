package memstore

import (
	"fmt"
	"testing"
	"testing/synctest"
	"time"
)

func TestSweep_ReclaimsExpired(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		s := New()
		for i := range 1000 {
			if err := s.Set(ctx, fmt.Sprint(i), []byte("v"), time.Second); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(2 * time.Minute)
		if err := s.Set(ctx, "fresh", []byte("v"), time.Minute); err != nil {
			t.Fatal(err)
		}
		s.mu.Lock()
		n := len(s.m)
		s.mu.Unlock()
		if n != 1 {
			t.Fatalf("map holds %d entries after the sweep, want 1", n)
		}
	})
}
