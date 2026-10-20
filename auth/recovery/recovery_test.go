package recovery_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/storetest"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/recovery"
)

func setup(t *testing.T) (*codes, *recovery.Codes, *storetest.Recorder, *recovery.Verifier) {
	t.Helper()
	generated := mustGenerate(t, 10)
	db := &codes{unused: map[string][]string{"user-1": generated.Hashes()}}
	rec := &storetest.Recorder{Next: memstore.New()}
	v, err := recovery.New(db.lookup, db.consume, rec)
	if err != nil {
		t.Fatal(err)
	}
	return db, generated, rec, v
}

func TestVerify_Success(t *testing.T) {
	db, generated, rec, v := setup(t)
	p, err := v.Verify(t.Context(), newPending(t, mustSessions(t, rec), "user-1"), generated.Plain()[0])
	if err != nil || p.Subject != "user-1" || !slices.Equal(p.Methods, []string{"pwd", "mfa"}) {
		t.Fatalf("Verify = %+v, %v", p, err)
	}
	if len(db.consumed) != 1 || db.consumed[0] != generated.Hashes()[0] {
		t.Fatalf("consumed = %v", db.consumed)
	}
	if _, err := v.Verify(t.Context(), newPending(t, mustSessions(t, rec), "user-1"), generated.Plain()[0]); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("second use: %v", err)
	}
}

func TestVerify_Normalization(t *testing.T) {
	_, generated, rec, v := setup(t)
	m := mustSessions(t, rec)
	plain := generated.Plain()
	variants := []string{
		strings.ToUpper(plain[1]),
		strings.ReplaceAll(plain[2], "-", " "),
		strings.ReplaceAll(plain[3], "-", ""),
		strings.NewReplacer("0", "o", "1", "l").Replace(plain[4]),
	}
	for _, c := range variants {
		if _, err := v.Verify(t.Context(), newPending(t, m, "user-1"), c); err != nil {
			t.Errorf("variant %q: %v", c, err)
		}
	}
}

func TestVerify_Malformed(t *testing.T) {
	_, _, rec, v := setup(t)
	p := newPending(t, mustSessions(t, rec), "user-1")
	var err error
	for _, c := range []string{"abcd", "abcd-efgh-ijkl-mnop-qrst", "uuuu-uuuu-uuuu-uuuu", "!!!!-!!!!-!!!!-!!!!", ""} {
		_, err = v.Verify(t.Context(), p, c)
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("code %q: %v", c, err)
		}
	}
	if !errors.Is(err, auth.ErrTooManyAttempts) {
		t.Fatalf("malformed codes were not counted: %v", err)
	}
}

func TestVerify_Consumed(t *testing.T) {
	_, generated, rec, _ := setup(t)
	m := mustSessions(t, rec)
	lookup := func(context.Context, string) ([]string, error) { return generated.Hashes(), nil }

	already := func(context.Context, string, string) error { return recovery.ErrNotFound }
	v, _ := recovery.New(lookup, already, rec)
	if _, err := v.Verify(t.Context(), newPending(t, m, "user-1"), generated.Plain()[0]); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("consume reporting already used: %v", err)
	}

	boom := errors.New("db")
	failing := func(context.Context, string, string) error { return boom }
	v, _ = recovery.New(lookup, failing, rec)
	if _, err := v.Verify(t.Context(), newPending(t, m, "user-1"), generated.Plain()[1]); !errors.Is(err, boom) || errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("consume failure: %v", err)
	}
}

func TestVerify_LookupError(t *testing.T) {
	db, generated, rec, v := setup(t)
	db.err = errors.New("db down")
	p := newPending(t, mustSessions(t, rec), "user-1")
	rec.Reset()
	if _, err := v.Verify(t.Context(), p, generated.Plain()[0]); !errors.Is(err, db.err) || errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("lookup failure: %v", err)
	}
	if n := rec.Count(storetest.OpIncr); n != 0 {
		t.Fatalf("%d increments after a lookup failure, want 0", n)
	}
}

func TestSecondFactorParallel(t *testing.T) {
	db, generated, rec, v := setup(t)
	db.racy = true
	m := mustSessions(t, rec)
	pendings := make([]auth.Pending, 50)
	for i := range pendings {
		pendings[i] = newPending(t, m, "user-1")
	}
	var (
		wg sync.WaitGroup
		ok atomic.Int32
	)
	for _, p := range pendings {
		wg.Go(func() {
			if _, err := v.Verify(t.Context(), p, generated.Plain()[5]); err == nil {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if n := ok.Load(); n != 1 {
		t.Fatalf("%d acceptances, want exactly 1", n)
	}
}

func TestRecovery_Config(t *testing.T) {
	db := &codes{}
	store := memstore.New()
	for name, build := range map[string]func() error{
		"nil lookup":  func() error { _, err := recovery.New(nil, db.consume, store); return err },
		"nil consume": func() error { _, err := recovery.New(db.lookup, nil, store); return err },
		"nil store":   func() error { _, err := recovery.New(db.lookup, db.consume, nil); return err },
	} {
		if err := build(); !errors.Is(err, auth.ErrInvalidConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
