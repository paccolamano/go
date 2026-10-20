package apikey_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/apikey"
	"github.com/paccolamano/go/auth/internal/crockford"
)

type ctxKey struct{}

func TestGenerate_Format(t *testing.T) {
	_, k := newKeys(t)
	key, s := k.Generate()
	re := regexp.MustCompile(`^acme_([0-9a-hjkmnp-tv-z]{16})_([0-9a-hjkmnp-tv-z]{52})$`)
	m := re.FindStringSubmatch(string(key))
	if m == nil {
		t.Fatalf("key %q has the wrong format", key)
	}
	if s.ID != m[1] {
		t.Fatalf("Stored.ID = %q, want %q", s.ID, m[1])
	}
	secret, err := crockford.Decode(m[2])
	sum := sha256.Sum256(secret)
	if err != nil || len(secret) != 32 || !bytes.Equal(s.Hash, sum[:]) {
		t.Fatalf("Stored.Hash is not the SHA-256 of the 32-byte secret")
	}
	if strings.Contains(fmt.Sprintf("%+v", s), m[2]) {
		t.Fatal("Stored contains the secret")
	}
	other, _ := k.Generate()
	if other == key {
		t.Fatal("two keys are equal")
	}
}

func TestGenerate_Redacted(t *testing.T) {
	_, k := newKeys(t)
	key, _ := k.Generate()
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("generated", "key", key)
	if !strings.Contains(buf.String(), "[REDACTED]") || strings.Contains(buf.String(), string(key)) {
		t.Fatalf("slog output %q", buf.String())
	}
	if fmt.Sprint(key) != string(key) {
		t.Fatal("fmt does not print the key")
	}
}

func TestAuthenticate_Success(t *testing.T) {
	tbl, k := newKeys(t)
	key := store(tbl, k, "client-9")
	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		r := bearer(t, scheme+" "+string(key))
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, "request"))
		p, err := k.Authenticate(r)
		if err != nil || p.Subject != "client-9" || len(p.Methods) != 0 {
			t.Fatalf("%s: %+v, %v", scheme, p, err)
		}
	}
	if tbl.ctxs[0].Value(ctxKey{}) != "request" || tbl.ids[0] != strings.Split(string(key), "_")[1] {
		t.Fatal("the lookup did not receive the request context and the identifier")
	}
}

func TestAuthenticate_Invalid(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tbl, k := newKeys(t)
		key, s := k.Generate()
		parts := strings.Split(string(key), "_")
		wrong, _ := k.Generate()
		wrongSecret := parts[0] + "_" + parts[1] + "_" + strings.Split(string(wrong), "_")[2]

		tbl.m[s.ID] = apikey.Account{Subject: "c", Hash: s.Hash}
		unknown, _ := k.Generate()
		for name, cred := range map[string]string{"wrong secret": wrongSecret, "unknown id": string(unknown)} {
			if _, err := k.Authenticate(bearer(t, "Bearer "+cred)); !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Errorf("%s: %v", name, err)
			}
		}
		for name, exp := range map[string]time.Time{"equal to now": time.Now(), "past": time.Now().Add(-time.Hour)} {
			tbl.m[s.ID] = apikey.Account{Subject: "c", Hash: s.Hash, ExpiresAt: exp}
			if _, err := k.Authenticate(bearer(t, "Bearer "+string(key))); !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Errorf("expiry %s: %v", name, err)
			}
		}
		tbl.m[s.ID] = apikey.Account{Subject: "c", Hash: s.Hash, ExpiresAt: time.Now().Add(time.Hour)}
		if _, err := k.Authenticate(bearer(t, "Bearer "+string(key))); err != nil {
			t.Errorf("future expiry: %v", err)
		}
		time.Sleep(time.Hour)
		if _, err := k.Authenticate(bearer(t, "Bearer "+string(key))); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("after the expiry: %v", err)
		}
	})
}

func TestAuthenticate_NotMine(t *testing.T) {
	_, k := newKeys(t)
	for _, v := range []string{"", "Bearer x.y.z", "Bearer other_0123456789abcdef_x", "Basic YTpi", "Bearer acmex_1_2"} {
		if _, err := k.Authenticate(bearer(t, v)); !errors.Is(err, auth.ErrNoCredentials) {
			t.Errorf("%q: %v, want ErrNoCredentials", v, err)
		}
	}
}

func TestAuthenticate_Malformed(t *testing.T) {
	_, k := newKeys(t)
	key := string(func() apikey.Key { k2, _ := k.Generate(); return k2 }())
	id := strings.Split(key, "_")[1]
	for _, v := range []string{
		"acme_",
		"acme_x_y",
		"acme_" + id,
		key + "0",
		key[:len(key)-1],
		key[:10] + "U" + key[11:],
		"acme_" + id[:15] + "_" + strings.Split(key, "_")[2],
	} {
		if _, err := k.Authenticate(bearer(t, "Bearer "+v)); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("%q: %v, want invalid", v, err)
		}
	}
}

func TestAuthenticate_LookupError(t *testing.T) {
	tbl, k := newKeys(t)
	key := store(tbl, k, "c")
	tbl.err = errors.New("db down")
	mw, _ := auth.New(k)
	rec := httptest.NewRecorder()
	mw(http.NotFoundHandler()).ServeHTTP(rec, bearer(t, "Bearer "+string(key)))
	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	tbl.err = nil
	empty, s := k.Generate()
	tbl.m[s.ID] = apikey.Account{Hash: s.Hash}
	if _, err := k.Authenticate(bearer(t, "Bearer "+string(empty))); err == nil || errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("empty subject: %v, want a contract error", err)
	}
}

func TestHeader_Custom(t *testing.T) {
	tbl, k := newKeys(t, apikey.WithHeader("X-API-Key"))
	key := store(tbl, k, "c")
	r := bearer(t, "")
	r.Header.Set("X-API-Key", " "+string(key)+" ")
	if p, err := k.Authenticate(r); err != nil || p.Subject != "c" {
		t.Fatalf("custom header: %v, %v", p, err)
	}
	if _, err := k.Authenticate(bearer(t, "Bearer "+string(key))); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("Authorization with a custom header configured: %v", err)
	}
	if got := k.Challenge(auth.ErrNoCredentials); got != `ApiKey header="X-API-Key"` {
		t.Fatalf("challenge = %q", got)
	}
}

func TestKeys_Challenge(t *testing.T) {
	_, k := newKeys(t)
	for cause, want := range map[error]string{
		auth.ErrNoCredentials:       "Bearer",
		auth.ErrInvalidCredentials:  `Bearer error="invalid_token"`,
		auth.ErrInsufficientMethods: "",
	} {
		if got := k.Challenge(cause); got != want {
			t.Errorf("Challenge(%v) = %q, want %q", cause, got, want)
		}
	}
}

func TestKeys_RequireMFA(t *testing.T) {
	tbl, k := newKeys(t)
	key := store(tbl, k, "c")
	mw, _ := auth.New(k)
	rec := httptest.NewRecorder()
	mw(auth.RequireMethods("mfa")(http.NotFoundHandler())).ServeHTTP(rec, bearer(t, "Bearer "+string(key)))
	if rec.Code != 403 {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestKeys_Config(t *testing.T) {
	lookup := (&table{}).lookup
	for _, prefix := range []string{"", "1acme", "ac_me", "ac-me", strings.Repeat("a", 33)} {
		if _, err := apikey.New(prefix, lookup); !errors.Is(err, auth.ErrInvalidConfig) {
			t.Errorf("prefix %q: %v", prefix, err)
		}
	}
	if _, err := apikey.New("Acme2", lookup); err != nil {
		t.Errorf("valid prefix: %v", err)
	}
	if _, err := apikey.New("acme", nil); !errors.Is(err, auth.ErrInvalidConfig) {
		t.Errorf("nil lookup: %v", err)
	}
	for _, h := range []string{"", "Authorization", "authorization", "X API"} {
		if _, err := apikey.New("acme", lookup, apikey.WithHeader(h)); !errors.Is(err, auth.ErrInvalidConfig) {
			t.Errorf("header %q: %v", h, err)
		}
	}
	if _, err := apikey.New("acme", lookup, nil); !errors.Is(err, auth.ErrInvalidConfig) {
		t.Errorf("nil option: %v", err)
	}
}
