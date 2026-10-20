package password_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/storetest"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/password"
)

func limited(t *testing.T, store auth.Store, max int) (*accounts, *password.Checker) {
	t.Helper()
	return newAccounts(t, password.WithAttemptLimit(password.AttemptLimit{Store: store, Max: max, Window: 15 * time.Minute}))
}

func TestAttemptLimit_Blocks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, c := limited(t, memstore.New(), 10)
		for _, user := range []string{"alice", "nobody"} {
			for i := range 10 {
				if _, err := c.Check(t.Context(), user, "wrong"); !errors.Is(err, auth.ErrInvalidCredentials) {
					t.Fatalf("%s failure %d: %v", user, i+1, err)
				}
			}
			if _, err := c.Check(t.Context(), user, "correct horse"); !errors.Is(err, auth.ErrTooManyAttempts) {
				t.Fatalf("%s eleventh check: %v, want ErrTooManyAttempts", user, err)
			}
		}
		time.Sleep(15 * time.Minute)
		if _, err := c.Check(t.Context(), "alice", "correct horse"); err != nil {
			t.Fatalf("after the window: %v", err)
		}
	})
}

func TestAttemptLimit_Clears(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, c := limited(t, memstore.New(), 10)
		for range 3 {
			_, _ = c.Check(t.Context(), "alice", "wrong")
		}
		if _, err := c.Check(t.Context(), "alice", "correct horse"); err != nil {
			t.Fatal(err)
		}
		for i := range 10 {
			if _, err := c.Check(t.Context(), "alice", "wrong"); !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("failure %d after a success: %v", i+1, err)
			}
		}
	})
}

func TestAttemptLimit_StoreError(t *testing.T) {
	_, c := limited(t, storetest.Failing{Next: memstore.New(), Err: errDB, Ops: []storetest.Op{storetest.OpIncr}}, 10)
	_, err := c.Check(t.Context(), "alice", "correct horse")
	if err == nil || errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrTooManyAttempts) {
		t.Fatalf("error = %v, want a store failure", err)
	}
}

func TestAttemptLimit_LookupFirst(t *testing.T) {
	rec := &storetest.Recorder{Next: memstore.New()}
	acc, c := limited(t, rec, 10)
	acc.err = errDB
	_, _ = c.Check(t.Context(), "alice", "correct horse")
	if n := rec.Count(storetest.OpIncr); n != 0 {
		t.Fatalf("Incr called %d times after a lookup failure, want 0", n)
	}
}

func TestAttemptLimit_Defaults(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, c := newAccounts(t, password.WithAttemptLimit(password.AttemptLimit{Store: memstore.New()}))
		for range 10 {
			_, _ = c.Check(t.Context(), "alice", "wrong")
		}
		if _, err := c.Check(t.Context(), "alice", "correct horse"); !errors.Is(err, auth.ErrTooManyAttempts) {
			t.Fatalf("eleventh check: %v", err)
		}
		time.Sleep(15 * time.Minute)
		if _, err := c.Check(t.Context(), "alice", "correct horse"); err != nil {
			t.Fatalf("after 15 minutes: %v", err)
		}
	})
}

func TestAttemptLimit_Config(t *testing.T) {
	lookup := (&accounts{}).lookup
	for _, l := range []password.AttemptLimit{
		{},
		{Store: memstore.New(), Max: -1},
		{Store: memstore.New(), Window: -time.Second},
	} {
		if _, err := password.NewChecker(lookup, password.WithAttemptLimit(l)); !errors.Is(err, auth.ErrInvalidConfig) {
			t.Errorf("%+v: error = %v, want ErrInvalidConfig", l, err)
		}
	}
}

func TestAttemptLimit_Keys(t *testing.T) {
	rec := &storetest.Recorder{Next: memstore.New()}
	_, c := limited(t, rec, 10)
	_, _ = c.Check(t.Context(), "alice", "wrong")
	_, _ = c.Check(t.Context(), "alice", "correct horse")
	for _, call := range rec.Calls() {
		if strings.Contains(call.Key, "alice") {
			t.Fatalf("key %q contains the username", call.Key)
		}
	}
}

func TestAttemptLimitParallel(t *testing.T) {
	_, c := limited(t, memstore.New(), 10)
	var (
		wg              sync.WaitGroup
		mu              sync.Mutex
		invalid, tooMny int
	)
	for range 50 {
		wg.Go(func() {
			_, err := c.Check(t.Context(), "alice", "wrong")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, auth.ErrTooManyAttempts):
				tooMny++
			case errors.Is(err, auth.ErrInvalidCredentials):
				invalid++
			default:
				t.Errorf("unexpected error %v", err)
			}
		})
	}
	wg.Wait()
	if invalid > 10 || invalid+tooMny != 50 {
		t.Fatalf("%d verified, %d blocked; want at most 10 verified", invalid, tooMny)
	}
}
