package memstore_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/memstore"
)

var _ auth.Store = (*memstore.Store)(nil)

func TestStore_Contract(t *testing.T) {
	ctx := t.Context()
	s := memstore.New()

	if _, err := s.Get(ctx, "k"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("Get(absent) error = %v, want ErrNotFound", err)
	}
	in := []byte("v1")
	if err := s.Set(ctx, "k", in, time.Minute); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X'
	got, err := s.Get(ctx, "k")
	if err != nil || string(got) != "v1" {
		t.Fatalf("Get = %q, %v; want v1", got, err)
	}
	got[0] = 'Y'
	if again, _ := s.Get(ctx, "k"); string(again) != "v1" {
		t.Fatalf("mutating the returned slice changed the value to %q", again)
	}
	if err := s.Set(ctx, "k", []byte("v2"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, "k"); string(got) != "v2" {
		t.Fatalf("Set did not replace: %q", got)
	}
	in = []byte("v3")
	if ok, err := s.Replace(ctx, "k", in, time.Minute); !ok || err != nil {
		t.Fatalf("Replace(present) = %v, %v; want true, nil", ok, err)
	}
	in[0] = 'X'
	if got, _ := s.Get(ctx, "k"); string(got) != "v3" {
		t.Fatalf("Replace stored %q, want v3", got)
	}
	if ok, err := s.Replace(ctx, "absent", []byte("v"), time.Minute); ok || err != nil {
		t.Fatalf("Replace(absent) = %v, %v; want false, nil", ok, err)
	}
	if _, err := s.Get(ctx, "absent"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("Replace(absent) created the key: Get = %v", err)
	}
	if err := s.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete(absent) = %v, want nil", err)
	}
	if err := s.Set(ctx, "k", nil, 0); err == nil {
		t.Error("Set with ttl 0 succeeded")
	}
	if _, err := s.Replace(ctx, "k", nil, 0); err == nil {
		t.Error("Replace with ttl 0 succeeded")
	}
	if _, err := s.Incr(ctx, "c", -time.Second); err == nil {
		t.Error("Incr with negative ttl succeeded")
	}
	_ = s.Set(ctx, "bytes", []byte("x"), time.Minute)
	if _, err := s.Incr(ctx, "bytes", time.Minute); err == nil {
		t.Error("Incr on a byte value succeeded")
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Get(cancelled, "k"); !errors.Is(err, context.Canceled) {
		t.Errorf("Get with cancelled ctx = %v", err)
	}
	if err := s.Set(cancelled, "k", nil, time.Minute); !errors.Is(err, context.Canceled) {
		t.Errorf("Set with cancelled ctx = %v", err)
	}
	if _, err := s.Replace(cancelled, "k", nil, time.Minute); !errors.Is(err, context.Canceled) {
		t.Errorf("Replace with cancelled ctx = %v", err)
	}
	if err := s.Delete(cancelled, "k"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete with cancelled ctx = %v", err)
	}
	if _, err := s.Incr(cancelled, "c", time.Minute); !errors.Is(err, context.Canceled) {
		t.Errorf("Incr with cancelled ctx = %v", err)
	}
}

func TestExpiry_TTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		s := memstore.New()
		if err := s.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
			t.Fatal(err)
		}
		time.Sleep(59 * time.Second)
		if _, err := s.Get(ctx, "k"); err != nil {
			t.Fatalf("Get after 59s = %v, want found", err)
		}
		time.Sleep(time.Second)
		if _, err := s.Get(ctx, "k"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("Get after 60s = %v, want ErrNotFound", err)
		}

		// Replace sets a new expiry, and never revives an expired key.
		if err := s.Set(ctx, "r", []byte("v"), time.Minute); err != nil {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Second)
		if ok, err := s.Replace(ctx, "r", []byte("w"), time.Minute); !ok || err != nil {
			t.Fatalf("Replace after 30s = %v, %v; want true, nil", ok, err)
		}
		time.Sleep(59 * time.Second)
		if got, err := s.Get(ctx, "r"); err != nil || string(got) != "w" {
			t.Fatalf("Get 89s after Set = %q, %v; want w", got, err)
		}
		time.Sleep(time.Second)
		if ok, err := s.Replace(ctx, "r", []byte("x"), time.Minute); ok || err != nil {
			t.Fatalf("Replace of an expired key = %v, %v; want false, nil", ok, err)
		}
		if _, err := s.Get(ctx, "r"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("Get after Replace of an expired key = %v, want ErrNotFound", err)
		}

		for i, want := range []int64{1, 2, 1} {
			if i > 0 {
				time.Sleep(5 * time.Second)
			}
			n, err := s.Incr(ctx, "c", 10*time.Second)
			if err != nil || n != want {
				t.Fatalf("Incr #%d = %d, %v; want %d", i+1, n, err, want)
			}
		}
	})
}

func TestIncrParallel_Atomic(t *testing.T) {
	ctx := t.Context()
	s := memstore.New()
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		got []int64
	)
	for range 50 {
		wg.Go(func() {
			n, err := s.Incr(ctx, "c", time.Minute)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			got = append(got, n)
			mu.Unlock()
		})
	}
	wg.Wait()
	slices.Sort(got)
	for i, n := range got {
		if n != int64(i+1) {
			t.Fatalf("values = %v, want 1..50 exactly once", got)
		}
	}
	if len(got) != 50 {
		t.Fatalf("got %d values, want 50", len(got))
	}
}

func TestReplaceParallel_NeverRevives(t *testing.T) {
	ctx := t.Context()
	s := memstore.New()
	for i := range 100 {
		if err := s.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Go(func() {
			if _, err := s.Replace(ctx, "k", []byte("w"), time.Minute); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			if err := s.Delete(ctx, "k"); err != nil {
				t.Error(err)
			}
		})
		wg.Wait()
		if _, err := s.Get(ctx, "k"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("run %d: key present after a concurrent Replace and Delete: %v", i, err)
		}
	}
}
