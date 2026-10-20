package password_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/password"
)

var errDB = errors.New("db down")

type ctxKey struct{}

func newAccounts(t *testing.T, opts ...password.Option) (*accounts, *password.Checker) {
	t.Helper()
	acc := &accounts{m: map[string]password.Account{}}
	c := mustChecker(t, acc.lookup, append([]password.Option{floor()}, opts...)...)
	acc.m["alice"] = password.Account{Subject: "user-1", Hash: mustHash(t, c, "correct horse")}
	return acc, c
}

func TestCheck_Success(t *testing.T) {
	acc, c := newAccounts(t)
	ctx := context.WithValue(t.Context(), ctxKey{}, "request")
	p, err := c.Check(ctx, "alice", "correct horse")
	if err != nil || p.Subject != "user-1" || len(p.Methods) != 1 || p.Methods[0] != "pwd" {
		t.Fatalf("Check = %+v, %v", p, err)
	}
	if acc.ctxs[0].Value(ctxKey{}) != "request" {
		t.Fatal("the request context did not reach the lookup")
	}
}

func TestCheck_Wrong(t *testing.T) {
	_, c := newAccounts(t)
	if _, err := c.Check(t.Context(), "alice", "wrong"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
}

func TestCheck_Unknown(t *testing.T) {
	_, c := newAccounts(t)
	if _, err := c.Check(t.Context(), "bob", "correct horse"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
}

func TestCheck_LookupError(t *testing.T) {
	acc, c := newAccounts(t)
	acc.err = errDB
	_, err := c.Check(t.Context(), "alice", "correct horse")
	if !errors.Is(err, errDB) || errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("error = %v, want the lookup error and not invalid", err)
	}
}

func TestCheck_EmptySubject(t *testing.T) {
	acc, c := newAccounts(t)
	acc.m["alice"] = password.Account{Hash: acc.m["alice"].Hash}
	if _, err := c.Check(t.Context(), "alice", "correct horse"); err == nil || errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("error = %v, want a contract error", err)
	}
}

func TestCheck_Malformed(t *testing.T) {
	acc, c := newAccounts(t)
	for _, h := range []string{
		"$argon2id$v=19$m=7168,t=5,p=1$bad",
		"$scrypt$ln=16,r=8,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=4194304,t=1,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5",
		"$2a$10$short",
		"plain-text",
	} {
		acc.m["alice"] = password.Account{Subject: "user-1", Hash: h}
		if _, err := c.Check(t.Context(), "alice", "correct horse"); err == nil || errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("hash %q: error = %v, want a non-credential error", h, err)
		}
	}
}

func TestLegacyHashes_Bcrypt(t *testing.T) {
	acc, c := newAccounts(t)
	h, _ := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	acc.m["legacy"] = password.Account{Subject: "user-2", Hash: string(h)}
	if _, err := c.Check(t.Context(), "legacy", "correct horse"); err != nil {
		t.Fatalf("bcrypt hash does not verify: %v", err)
	}

	pw72 := strings.Repeat("x", 72)
	h72, _ := bcrypt.GenerateFromPassword([]byte(pw72), bcrypt.MinCost)
	acc.m["long"] = password.Account{Subject: "user-3", Hash: string(h72)}
	if _, err := c.Check(t.Context(), "long", pw72); err != nil {
		t.Fatalf("72-byte password: %v", err)
	}
	if _, err := c.Check(t.Context(), "long", pw72+"y"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("73-byte password with a correct 72-byte prefix: %v, want invalid", err)
	}
}

func TestRehash_Called(t *testing.T) {
	type call struct{ subject, hash string }
	var got []call
	acc, c := newAccounts(t, password.WithRehash(func(_ context.Context, subject, hash string) {
		got = append(got, call{subject, hash})
	}))
	old := mustChecker(t, acc.lookup, password.WithParams(password.Params{Memory: 7168, Iterations: 6, Parallelism: 1}))
	bc, _ := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	for name, h := range map[string]string{"bcrypt": string(bc), "argon2id t=6": mustHash(t, old, "correct horse")} {
		got = nil
		acc.m["alice"] = password.Account{Subject: "user-1", Hash: h}
		if _, err := c.Check(t.Context(), "alice", "correct horse"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 1 || got[0].subject != "user-1" || !strings.HasPrefix(got[0].hash, "$argon2id$v=19$m=7168,t=5,p=1$") {
			t.Fatalf("%s: rehash calls = %+v", name, got)
		}
		acc.m["alice"] = password.Account{Subject: "user-1", Hash: got[0].hash}
		got = nil
		if _, err := c.Check(t.Context(), "alice", "correct horse"); err != nil || len(got) != 0 {
			t.Fatalf("%s: new hash: %v, %d rehash calls", name, err, len(got))
		}
	}
}

func TestRehash_NotCalled(t *testing.T) {
	calls := 0
	acc, c := newAccounts(t, password.WithRehash(func(context.Context, string, string) { calls++ }))
	bc, _ := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	acc.m["legacy"] = password.Account{Subject: "user-2", Hash: string(bc)}
	_, _ = c.Check(t.Context(), "legacy", "wrong")
	_, _ = c.Check(t.Context(), "alice", "correct horse")
	if calls != 0 {
		t.Fatalf("rehash called %d times, want 0", calls)
	}
}

func TestLength_Limits(t *testing.T) {
	acc, c := newAccounts(t, password.WithMaxLength(256))
	before := acc.calls
	if _, err := c.Check(t.Context(), "alice", strings.Repeat("x", 257)); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("257 bytes: %v, want invalid", err)
	}
	if acc.calls != before {
		t.Fatal("the lookup was called for an overlong password")
	}
	long := strings.Repeat("𝄞", 64)
	acc.m["clef"] = password.Account{Subject: "user-4", Hash: mustHash(t, c, long)}
	if len(long) != 256 {
		t.Fatalf("fixture is %d bytes", len(long))
	}
	if _, err := c.Check(t.Context(), "clef", long); err != nil {
		t.Fatalf("64 four-byte characters: %v", err)
	}
	for _, c2 := range [][2]string{{"alice", ""}, {"", "correct horse"}} {
		if _, err := c.Check(t.Context(), c2[0], c2[1]); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("Check(%q, %q) = %v, want invalid", c2[0], c2[1], err)
		}
	}
}

func TestCheck_Config(t *testing.T) {
	lookup := (&accounts{}).lookup
	for name, build := range map[string]func() error{
		"nil lookup":      func() error { _, err := password.NewChecker(nil); return err },
		"nil rehash":      func() error { _, err := password.NewChecker(lookup, password.WithRehash(nil)); return err },
		"max length 255":  func() error { _, err := password.NewChecker(lookup, password.WithMaxLength(255)); return err },
		"max length 4097": func() error { _, err := password.NewChecker(lookup, password.WithMaxLength(4097)); return err },
		"weak params": func() error {
			_, err := password.NewChecker(lookup, password.WithParams(password.Params{Memory: 4096, Iterations: 1, Parallelism: 1}))
			return err
		},
		"nil option": func() error { _, err := password.NewChecker(lookup, nil); return err },
	} {
		if err := build(); !errors.Is(err, auth.ErrInvalidConfig) {
			t.Errorf("%s: error = %v, want ErrInvalidConfig", name, err)
		}
	}
}
