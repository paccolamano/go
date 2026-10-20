package totp_test

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/storetest"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/totp"
)

var errDown = errors.New("down")

func setup(t *testing.T) (*secrets, *storetest.Recorder, *totp.Verifier) {
	t.Helper()
	s := &secrets{m: map[string][]byte{"user-1": secret}}
	rec := &storetest.Recorder{Next: memstore.New()}
	return s, rec, mustVerifier(t, s.lookup, rec)
}

func TestVerify_Success(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, rec, v := setup(t)
		p, err := v.Verify(t.Context(), newPending(t, mustSessions(t, rec), "user-1"), code(0))
		if err != nil || p.Subject != "user-1" || !slices.Equal(p.Methods, []string{"pwd", "otp", "mfa"}) {
			t.Fatalf("Verify = %+v, %v", p, err)
		}
	})
}

func TestVerify_Window(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, rec, v := setup(t)
		m := mustSessions(t, rec)
		for offset, ok := range map[int64]bool{-2: false, -1: true, 0: true, 1: true, 2: false} {
			_, err := v.Verify(t.Context(), newPending(t, m, "user-1"), code(offset))
			if (err == nil) != ok {
				t.Errorf("offset %d: %v, want accepted %v", offset, err, ok)
			}
		}
	})
}

func TestVerify_Replay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, rec, v := setup(t)
		m := mustSessions(t, rec)
		p := newPending(t, m, "user-1")
		c := code(0)
		if _, err := v.Verify(t.Context(), p, c); err != nil {
			t.Fatal(err)
		}
		if _, err := v.Verify(t.Context(), p, c); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("same code again: %v", err)
		}
		if _, err := v.Verify(t.Context(), newPending(t, m, "user-1"), c); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("same code from another pending login: %v", err)
		}
	})
}

func TestVerify_Malformed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, rec, v := setup(t)
		p := newPending(t, mustSessions(t, rec), "user-1")
		var err error
		for _, c := range []string{"12345", "1234567", "12a456", " 123456", "１２３４５６"} {
			_, err = v.Verify(t.Context(), p, c)
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("code %q: %v, want invalid", c, err)
			}
		}
		if !errors.Is(err, auth.ErrTooManyAttempts) {
			t.Fatalf("five malformed codes did not exhaust the budget: %v", err)
		}
	})
}

func TestVerify_NotEnrolled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, rec, v := setup(t)
		delete(s.m, "user-1")
		p := newPending(t, mustSessions(t, rec), "user-1")
		rec.Reset()
		if _, err := v.Verify(t.Context(), p, code(0)); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("not enrolled: %v, want ErrNotFound", err)
		}
		if n := rec.Count(storetest.OpIncr); n != 0 {
			t.Fatalf("%d increments, want the attempt not counted", n)
		}
	})
}

func TestVerify_LookupError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, rec, v := setup(t)
		s.err = errDown
		p := newPending(t, mustSessions(t, rec), "user-1")
		rec.Reset()
		if _, err := v.Verify(t.Context(), p, code(0)); !errors.Is(err, errDown) || errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("lookup failure: %v", err)
		}
		if n := rec.Count(storetest.OpIncr); n != 0 {
			t.Fatalf("%d increments after a lookup failure, want 0", n)
		}
	})
}

func TestVerify_StoreError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &secrets{m: map[string][]byte{"user-1": secret}}
		store := memstore.New()
		p := newPending(t, mustSessions(t, storetest.Failing{Next: store, Err: errDown, Ops: []storetest.Op{storetest.OpIncr}}), "user-1")
		v := mustVerifier(t, s.lookup, store)
		if _, err := v.Verify(t.Context(), p, code(0)); !errors.Is(err, errDown) || errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("store failure while counting: %v", err)
		}
	})
}

func TestEnrolled_States(t *testing.T) {
	s, _, v := setup(t)
	if ok, err := v.Enrolled(t.Context(), "user-1"); !ok || err != nil {
		t.Errorf("enrolled: %v, %v", ok, err)
	}
	if ok, err := v.Enrolled(t.Context(), "user-2"); ok || err != nil {
		t.Errorf("not enrolled: %v, %v", ok, err)
	}
	s.err = errDown
	if ok, err := v.Enrolled(t.Context(), "user-1"); ok || !errors.Is(err, errDown) {
		t.Errorf("lookup failure: %v, %v", ok, err)
	}
}

func TestConfirm_FirstCode(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, rec, v := setup(t)
		if err := v.Confirm(t.Context(), "user-1", secret, "000000"); !errors.Is(err, auth.ErrInvalidCredentials) && code(0) != "000000" {
			t.Fatalf("wrong first code: %v", err)
		}
		c := code(0)
		if err := v.Confirm(t.Context(), "user-1", secret, c); err != nil {
			t.Fatalf("correct first code: %v", err)
		}
		if _, err := v.Verify(t.Context(), newPending(t, mustSessions(t, rec), "user-1"), c); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("the confirmation code was accepted again: %v", err)
		}
		if err := v.Confirm(t.Context(), "user-1", secret[:15], code(1)); err == nil {
			t.Fatal("a 15-byte secret was accepted")
		}
	})
}

func TestSecondFactorParallel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, rec, v := setup(t)
		m := mustSessions(t, rec)
		pendings := make([]auth.Pending, 50)
		for i := range pendings {
			pendings[i] = newPending(t, m, "user-1")
		}
		c := code(0)
		var (
			wg sync.WaitGroup
			ok atomic.Int32
		)
		for _, p := range pendings {
			wg.Go(func() {
				if _, err := v.Verify(t.Context(), p, c); err == nil {
					ok.Add(1)
				}
			})
		}
		wg.Wait()
		if n := ok.Load(); n != 1 {
			t.Fatalf("%d acceptances, want exactly 1", n)
		}
	})
}

func TestTOTP_Config(t *testing.T) {
	s := &secrets{}
	if _, err := totp.New(nil, memstore.New()); !errors.Is(err, auth.ErrInvalidConfig) {
		t.Errorf("nil lookup: %v", err)
	}
	if _, err := totp.New(s.lookup, nil); !errors.Is(err, auth.ErrInvalidConfig) {
		t.Errorf("nil store: %v", err)
	}
}
