# auth

Authentication of HTTP requests: passwords, sessions, a second factor with TOTP and recovery
codes, HTTP Basic, API keys and JWT. The library owns protocol and cryptography; your database
stays yours.

```bash
go get github.com/paccolamano/go/auth
```

Minimum Go version: 1.26

It handles the parts that are easy to get wrong:

- a nonexistent user and a wrong password take the same time;
- an unreachable database or store answers 500, never 401;
- passwords are hashed with argon2id; legacy bcrypt hashes still verify and are upgraded, and
  passwords over 72 bytes never match them;
- session identifiers change at every login, travel in a `__Host-` cookie and are stored only
  as hashes;
- every session of a user can be revoked at once, on every instance, for example after a password
  change;
- one-time codes have an attempt limit and cannot be replayed, even in parallel;
- a token's `alg` never chooses its key, `none` is rejected, and `exp` is required;
- API keys are stored as hashes, so a database backup cannot be used to call the service.

## Usage

You provide two integration points: lookup functions that read your database, and a
key-value `auth.Store` for the library's short-lived state.

```go
store := redisStore{C: rdb} // see "Store"

checker, _ := password.NewChecker(func(ctx context.Context, username string) (password.Account, error) {
	u, err := db.UserByUsername(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		return password.Account{}, password.ErrNotFound
	}
	if err != nil {
		return password.Account{}, err // becomes 500, never 401
	}
	return password.Account{Subject: u.ID, Hash: u.PasswordHash}, nil
})
sessions, _ := session.New(store)
otp, _ := totp.New(lookupTOTPSecret, store)
keys, _ := apikey.New("acme", lookupAPIKey)
tokens, _ := jwt.New(
	jwt.WithIssuer("https://id.example.com"),
	jwt.WithAudience("orders-api"),
	jwt.WithJWKS("https://id.example.com/.well-known/jwks.json"),
)

authn, _ := auth.New(auth.Any(sessions, keys, tokens))

mux := http.NewServeMux()
mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
	p, err := checker.Check(r.Context(), r.FormValue("username"), r.FormValue("password"))
	if err != nil {
		http.Error(w, "wrong username or password", http.StatusUnauthorized)
		return
	}
	if enrolled, _ := otp.Enrolled(r.Context(), p.Subject); enrolled {
		_ = sessions.IssuePending(w, r, p) // authenticates nothing yet
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
	if err != nil { // after five wrong codes, errors.Is(err, auth.ErrTooManyAttempts)
		http.Error(w, "wrong code", http.StatusUnauthorized)
		return
	}
	_ = sessions.Issue(w, r, p) // new identifier; the pending login is deleted
})
mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) { _ = sessions.Revoke(w, r) })
mux.Handle("GET /orders", authn(ordersHandler))
mux.Handle("GET /admin", authn(auth.RequireMethods("mfa")(adminHandler)))
```

Handlers read the principal with `auth.FromContext(r.Context())`: its `Subject` and the RFC 8176
`Methods` used (`pwd`, `otp`, `mfa`). `jwt.Claims(p)` returns the claims of a token, such as roles.
Authorization (roles, permissions) stays in your application.

`auth.WithRejectFunc` replaces the plain-text refusals, for example with an RFC 9457 problem
document; `auth.WithAnonymous` lets requests without credentials through.

## Store

`memstore` keeps state in memory, for tests and single-instance deployments. For several
instances, adapt Redis in about thirty lines with `github.com/redis/go-redis/v9` (this module does
not depend on it):

```go
// redisStore adapts a go-redis client to auth.Store.
type redisStore struct{ C redis.UniversalClient }

func (s redisStore) Get(ctx context.Context, key string) ([]byte, error) {
	b, err := s.C.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, auth.ErrNotFound
	}
	return b, err
}

func (s redisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.C.Set(ctx, key, value, ttl).Err()
}

// Replace writes only over an existing key: SET with XX.
func (s redisStore) Replace(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	return s.C.SetXX(ctx, key, value, ttl).Result()
}

func (s redisStore) Delete(ctx context.Context, key string) error {
	return s.C.Del(ctx, key).Err()
}

// incr sets the expiry only when the counter is created, atomically.
var incr = redis.NewScript(`local n = redis.call("INCR", KEYS[1])
if n == 1 then redis.call("PEXPIRE", KEYS[1], ARGV[1]) end
return n`)

func (s redisStore) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	return incr.Run(ctx, s.C, []string{key}, ttl.Milliseconds()).Int64()
}
```

`INCR` and `PEXPIRE` run in one script, which Redis executes atomically: two separate commands
would leave a counter without expiry if the client died between them. `SET` with `XX` checks that
the key exists and writes it in one command, so a session renewed while a logout deletes it stays
deleted; `SetXX` reports `false`, with no error, when the key is absent. Every operation touches one
key, so the adapter works with Redis Cluster. Keys start with `auth:`; prefix them in the adapter
if several applications share one database.

The store must keep every key until it expires, and a read must see every write that completed
before it. A revocation that the store evicts early stops revoking, and evictions also reset
attempt limits: keep Redis on its default `maxmemory-policy`, `noeviction`, since the `volatile-*`
policies can evict any key of this module. Read from the primary, not from replicas, so that a
session issued right after a revocation sees it.

## Passwords

`password.Hash` and `Checker.Hash` produce argon2id hashes in PHC format with the OWASP defaults:
19 MiB, 2 iterations, parallelism 1 (about 17 ms on a laptop CPU). Raise them with
`password.WithParams`, for example to the RFC 9106 option `{Memory: 65536, Iterations: 3,
Parallelism: 4}`; configurations weaker than the OWASP ones are rejected. Hashes that use bcrypt or
older parameters still verify; pass `password.WithRehash` to store a fresh hash at the next
successful login.

Passwords longer than the maximum (1,024 bytes by default) are rejected, never truncated.

`password.WithAttemptLimit` blocks a username after 10 failures in 15 minutes (configurable),
counting nonexistent usernames too so that the limit reveals nothing. Pass usernames in their
canonical form, for example lower-cased, when your lookup ignores case. The trade-off: an attacker
can block a victim's logins for the window, which is why it is short. Combine it with per-IP
limits from `middleware/ratelimit`.

## Sessions and CSRF

Cookies are `__Host-session` and `__Host-session-pending`, with `Secure`, `HttpOnly` and
`SameSite=Lax`. Sessions expire after 8 hours, or after 30 idle minutes; activity renews them at
most once every 7.5 minutes, so most requests cost two store reads, the session and its user's
revocation state, and no write.

Cookie sessions need protection against cross-site request forgery. Wrap the routes that change
state with the standard library:

```go
csrf := http.NewCrossOriginProtection()
handler := csrf.Handler(mux)
```

Browsers accept `Secure` and `__Host-` cookies on `http://localhost`, so local development works
without TLS.

## Revoking every session of a user

`sessions.RevokeSubject(ctx, subject)` ends every session and pending login of a user, on every
instance: call it after a password change, when an account is disabled, or to log out of every
device. It needs no request, so an administrative action or a background job can call it too. To
keep the current browser signed in, issue a new session right after:

```go
mux.Handle("POST /password", authn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	// Check the current password and store the new hash here.
	if err := sessions.RevokeSubject(r.Context(), p.Subject); err != nil {
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	_ = sessions.Issue(w, r, p) // a new identifier, issued after the revocation
})))
```

Every request checks the user's revocation state, one more store read. A login whose password was
checked before a revocation, and whose session is issued after it, is not revoked: call `Issue`
right after checking the password. A revocation is kept for the session lifetime plus one hour,
then expires; sessions issued after it keep working.

Sessions issued by 0.2.0 stay valid until their user is revoked. During a rolling upgrade from
0.2.0, or after a rollback to it, instances running 0.2.0 do not check revocations.

## Second factor

`totp.NewEnrollment(issuer, account)` returns a secret and its `otpauth://` URI, to show as a QR
code. Keep the secret as a candidate; store it as active only after `Verifier.Confirm` accepts a
first code. `recovery.Generate(recovery.DefaultCount)` returns single-use codes: show `Plain()`
once and store only `Hashes()`. Both kinds of code share five attempts per pending login, and a
code is accepted at most once, even in parallel. A recovery code adds only `mfa` to the methods,
since RFC 8176 has no value for it.

## HTTP Basic

`basic.New(checker, basic.WithRealm("api"))` verifies a password at every request: each one costs
the CPU and memory of an argon2id computation, so limit the concurrency and the rate of the routes
that accept Basic (`middleware/maxconcurrency`, `middleware/ratelimit`). Basic has no second
factor: an account enrolled in TOTP can still use Basic on routes that do not require `mfa`.

## API keys

`keys.Generate()` returns a key of the form `acme_<id>_<secret>`, to show once, and the record to
store: the identifier and the SHA-256 of the 256-bit secret. Clients send it as
`Authorization: Bearer acme_<id>_<secret>`, or in the header set with `apikey.WithHeader`. A key
deleted from your database, or past its `ExpiresAt`, is rejected.

## JWT

The verifier binds each key to one algorithm (RS256, PS256, ES256, EdDSA or HS256), requires `exp`,
`iss` and `aud`, rejects `crit` and encrypted tokens, and can require a type with
`jwt.WithType("at+jwt")`. The key set is downloaded at the first verification and cached according
to `Cache-Control` (between 1 minute and 24 hours). A token with an unknown key identifier triggers a
new download at most once every 30 seconds (`jwt.WithRefreshInterval`), so a newly rotated key may be
rejected for up to that long. An unreachable key server answers 500, and keys downloaded earlier stay
in use while it is down.

## Middleware order

Place CORS before authentication, so that preflight requests, which carry no credentials, are
answered first. Rate limiting and concurrency limits go before authentication on the routes that
check passwords:

```text
realip → requestid → accesslog → recovery → secureheaders → cors → ratelimit → maxconcurrency → auth → handler
```

## Caveats

- Verifying a bcrypt hash takes a different time from the argon2id dummy hash used for unknown
  users: until an account's hash is upgraded at its next login, timing can reveal that it exists.
- Sessions and pending logins live in the store: with `memstore`, a restart logs everyone out.
