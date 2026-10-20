package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/apikey"
	"github.com/paccolamano/go/auth/internal/hotp"
	"github.com/paccolamano/go/auth/jwt"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/password"
	"github.com/paccolamano/go/auth/session"
	"github.com/paccolamano/go/auth/totp"
)

// demoDB stands in for the application's database.
type demoDB struct {
	users   map[string]password.Account // username → account
	secrets map[string][]byte           // subject → active TOTP secret
	keys    map[string]apikey.Account   // API key id → account
}

// demoApp builds the application of the README: password login with sessions,
// an optional TOTP second factor, API keys and tokens, all through one
// authentication middleware.
func demoApp(db *demoDB) http.Handler {
	store := memstore.New() // Redis in production
	checker, _ := password.NewChecker(func(_ context.Context, u string) (password.Account, error) {
		if a, ok := db.users[u]; ok {
			return a, nil
		}
		return password.Account{}, password.ErrNotFound
	})
	sessions, _ := session.New(store)
	otp, _ := totp.New(func(_ context.Context, s string) ([]byte, error) {
		if b, ok := db.secrets[s]; ok {
			return b, nil
		}
		return nil, totp.ErrNotFound
	}, store)
	keys, _ := apikey.New("acme", func(_ context.Context, id string) (apikey.Account, error) {
		if a, ok := db.keys[id]; ok {
			return a, nil
		}
		return apikey.Account{}, apikey.ErrNotFound
	})
	idpKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tokens, _ := jwt.New(jwt.WithIssuer("https://id.example.com"), jwt.WithAudience("orders-api"),
		jwt.WithKey("k1", "ES256", &idpKey.PublicKey)) // jwt.WithJWKS(url) in production

	mw, err := auth.New(auth.Any(sessions, keys, tokens))
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		p, err := checker.Check(r.Context(), r.FormValue("username"), r.FormValue("password"))
		if err != nil {
			http.Error(w, "wrong username or password", http.StatusUnauthorized)
			return
		}
		if enrolled, _ := otp.Enrolled(r.Context(), p.Subject); enrolled {
			_ = sessions.IssuePending(w, r, p)
			http.Redirect(w, r, "/login/otp", http.StatusSeeOther)
			return
		}
		_ = sessions.Issue(w, r, p)
	})
	mux.HandleFunc("POST /login/otp", func(w http.ResponseWriter, r *http.Request) {
		pending, err := sessions.Pending(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		p, err := otp.Verify(r.Context(), pending, r.FormValue("code"))
		switch {
		case errors.Is(err, auth.ErrTooManyAttempts):
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		case err != nil:
			http.Error(w, "wrong code", http.StatusUnauthorized)
		default:
			_ = sessions.Issue(w, r, p)
		}
	})
	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) { _ = sessions.Revoke(w, r) })
	mux.Handle("GET /orders", mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		_, _ = fmt.Fprintf(w, "orders of %s", p.Subject)
	})))
	mux.Handle("GET /admin", mw(auth.RequireMethods("mfa")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "admin")
	}))))
	return mux
}

// browser keeps cookies between requests, as a browser does.
type browser struct {
	app     http.Handler
	cookies map[string]string
}

func (b *browser) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, value := range b.cookies {
		r.Header.Add("Cookie", name+"="+value)
	}
	rec := httptest.NewRecorder()
	b.app.ServeHTTP(rec, r)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c.Value
		}
	}
	return rec
}

func newDemoDB() *demoDB {
	hash, _ := password.Hash("correct horse")
	return &demoDB{
		users: map[string]password.Account{
			"alice": {Subject: "user-1", Hash: hash},
			"bob":   {Subject: "user-2", Hash: hash},
		},
		secrets: map[string][]byte{"user-1": []byte("alice's 20-byte key!")},
		keys:    map[string]apikey.Account{},
	}
}

func ExampleNew() {
	db := newDemoDB()
	app := demoApp(db)
	alice := &browser{app: app, cookies: map[string]string{}}

	// Password first: alice is enrolled, so she gets a pending login.
	rec := alice.do("POST", "/login", url.Values{"username": {"alice"}, "password": {"correct horse"}})
	fmt.Println("login:", rec.Code, rec.Header().Get("Location"))
	fmt.Println("orders before the code:", alice.do("GET", "/orders", nil).Code)

	// Then the code from her authenticator app.
	code := hotp.Value(hotp.SHA1, db.secrets["user-1"], uint64(time.Now().Unix()/30), 6)
	fmt.Println("code:", alice.do("POST", "/login/otp", url.Values{"code": {code}}).Code)
	fmt.Println("orders:", alice.do("GET", "/orders", nil).Body.String())

	// A client without credentials learns how to authenticate.
	rec = (&browser{app: app, cookies: map[string]string{}}).do("GET", "/orders", nil)
	fmt.Println("anonymous:", rec.Code, rec.Result().Header.Values("WWW-Authenticate"))
	// Output:
	// login: 303 /login/otp
	// orders before the code: 401
	// code: 200
	// orders: orders of user-1
	// anonymous: 401 [Cookie cookie-name="__Host-session" Bearer]
}

func ExampleRequireMethods() {
	db := newDemoDB()
	app := demoApp(db)

	// bob has no second factor: his session holds only "pwd".
	bob := &browser{app: app, cookies: map[string]string{}}
	bob.do("POST", "/login", url.Values{"username": {"bob"}, "password": {"correct horse"}})
	fmt.Println("bob /admin:", bob.do("GET", "/admin", nil).Code)

	// alice passed TOTP: her session holds "pwd", "otp" and "mfa".
	alice := &browser{app: app, cookies: map[string]string{}}
	alice.do("POST", "/login", url.Values{"username": {"alice"}, "password": {"correct horse"}})
	code := hotp.Value(hotp.SHA1, db.secrets["user-1"], uint64(time.Now().Unix()/30), 6)
	alice.do("POST", "/login/otp", url.Values{"code": {code}})
	fmt.Println("alice /admin:", alice.do("GET", "/admin", nil).Code)
	// Output:
	// bob /admin: 403
	// alice /admin: 200
}
