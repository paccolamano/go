package auth_test

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/paccolamano/go/auth"
)

func TestAny_Order(t *testing.T) {
	var cCalls atomic.Int32
	a := stub{err: auth.ErrNoCredentials}
	b := stub{p: principal("b")}
	c := stub{p: principal("c"), calls: &cCalls}
	rec, p, _ := serve(t, mustNew(t, auth.Any(a, b, c)), newRequest(t))
	if rec.Code != 200 || p == nil || p.Subject != "b" {
		t.Fatalf("got %d %v, want B's principal", rec.Code, p)
	}
	if cCalls.Load() != 0 {
		t.Fatal("C was consulted")
	}
}

func TestAny_InvalidStops(t *testing.T) {
	var cCalls atomic.Int32
	rec, _, _ := serve(t, mustNew(t, auth.Any(stub{err: auth.ErrNoCredentials}, stub{err: auth.ErrInvalidCredentials}, stub{p: principal("c"), calls: &cCalls})), newRequest(t))
	if rec.Code != 401 || cCalls.Load() != 0 {
		t.Fatalf("got %d with %d calls to C, want 401 and 0", rec.Code, cCalls.Load())
	}
}

func TestAny_FailureStops(t *testing.T) {
	var cCalls atomic.Int32
	rec, _, _ := serve(t, mustNew(t, auth.Any(stub{err: auth.ErrNoCredentials}, stub{err: errDBDown}, stub{p: principal("c"), calls: &cCalls})), newRequest(t))
	if rec.Code != 500 || cCalls.Load() != 0 {
		t.Fatalf("got %d with %d calls to C, want 500 and 0", rec.Code, cCalls.Load())
	}
}

func TestAny_AllNone(t *testing.T) {
	_, err := auth.Any(stub{err: auth.ErrNoCredentials}, plain{err: auth.ErrNoCredentials}).Authenticate(newRequest(t))
	if !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("error = %v, want ErrNoCredentials", err)
	}
}

func TestAny_Nested(t *testing.T) {
	a := stub{err: auth.ErrNoCredentials, challenges: map[error]string{auth.ErrNoCredentials: "A"}}
	b := stub{err: auth.ErrNoCredentials, challenges: map[error]string{auth.ErrNoCredentials: "B"}}
	c := stub{err: auth.ErrNoCredentials, challenges: map[error]string{auth.ErrNoCredentials: "C"}}
	flat, _, _ := serve(t, mustNew(t, auth.Any(a, b, c)), newRequest(t))
	nested, _, _ := serve(t, mustNew(t, auth.Any(a, auth.Any(b, c))), newRequest(t))
	if got, want := challenges(nested), challenges(flat); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("nested challenges %q, flat %q", got, want)
	}

	p, err := auth.Any(a, auth.Any(b, stub{p: principal("deep")})).Authenticate(newRequest(t))
	if err != nil || p.Subject != "deep" {
		t.Fatalf("nested Authenticate = %v, %v", p, err)
	}
}

func TestAny_Standalone(t *testing.T) {
	_, err := auth.Any(stub{err: auth.ErrInvalidCredentials}).Authenticate(newRequest(t))
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := auth.Any(stub{}).Authenticate(newRequest(t)); err == nil || errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("nil/nil gave %v, want a contract error", err)
	}
}
