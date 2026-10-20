# Contract: Store

**API**: [public-api.md](public-api.md) | **Data model**: [../data-model.md](../data-model.md) §2 | **Research**: [../research.md](../research.md) R-08, R-09

`auth.Store` is the only interface the developer implements for the library's state. This
contract is what an adapter must guarantee, and what the module relies on.

## 1. Operations

| Operation | Must | Must not |
|-----------|------|----------|
| `Get(ctx, key)` | return the bytes last `Set` for `key`, or `auth.ErrNotFound` (possibly wrapped) when the key is absent or its TTL has elapsed | return a value whose TTL has elapsed |
| `Set(ctx, key, value, ttl)` | store `value` under `key`, replacing any previous value and TTL; `ttl > 0` | keep a reference to `value` that the caller can later mutate (in-process stores) |
| `Delete(ctx, key)` | remove the key; return nil when the key is absent | fail on absent keys |
| `Incr(ctx, key, ttl)` | atomically add 1 and return the new value. When the key is absent or expired, create it with value 1 and expiry `now + ttl`. `ttl > 0` | extend the expiry of an existing counter; lose or duplicate increments under concurrency |

Further rules:

- **Atomicity**: N concurrent `Incr` calls on the same absent key return each value from 1 to N
  exactly once (FR-021).
- **Errors**: any failure other than "not found" is returned as an error. The module turns it
  into 500 and never into 401 (FR-014).
- **Context**: every operation honors cancellation of `ctx`.
- **Separation**: the module never calls `Get` on a key it increments, and never calls `Incr`
  on a key it `Set`. An adapter needs no rule for mixing the two.

## 2. Keys

Every key the module uses has the form `auth:<kind>:<digest>[:<suffix>]`:

- `<digest>` is `base64.RawURLEncoding(sha256(input))`, 43 characters from `[A-Za-z0-9_-]`;
- the whole key is ASCII, has no spaces, and is at most 80 bytes.

| Kind | Digest input | Suffix | Operation | TTL |
|------|--------------|--------|-----------|-----|
| `session` | session identifier (the cookie value) | none | `Get`, `Set`, `Delete` | `min(rw + idle, exp) − now` |
| `pending` | pending identifier | none | `Get`, `Set`, `Delete` | pending lifetime |
| `pending-attempts` | pending identifier | none | `Incr` | pending lifetime |
| `password-attempts` | username as passed to `Check` | none | `Incr`, `Delete` | attempt window |
| `totp-used` | subject | `:<step>` (decimal) | `Incr` | 2 min |
| `recovery-used` | subject, a NUL byte, then the code hash | none | `Incr` | 10 min |

No key contains an identifier, a username, a subject or any secret in clear (FR-022, SC-003).
An adapter may add its own prefix, for example `myapp:`, to share one database between
applications.

## 3. Values

Only `session` and `pending` keys carry values. Each value is a UTF-8 JSON object whose `v` is
the format version:

```json
{"v":1,"sub":"user-42","amr":["pwd","otp","mfa"],"iat":1792396800,"exp":1792425600,"rw":1792397700}
```

```json
{"v":1,"sub":"user-42","amr":["pwd"],"iat":1792396800}
```

- Times are Unix seconds.
- A value that does not decode, or whose `v` is not 1, makes the operation fail with an error,
  answered with 500. Such a value never authenticates a request.
- Values contain no secrets: the subject, the methods and times only.

Counters are whatever the store's increment produces. The module never reads them.

## 4. In-memory store (`memstore`)

- It implements §1 exactly, using `time.Now` for expiry, which `testing/synctest` virtualizes.
- `Set` and `Get` copy the bytes.
- `Incr` on a key that holds bytes returns an error, a defensive check that the module never
  triggers.
- `ttl <= 0` returns an error.
- **Expiry**: an expired entry is never returned. Entries are removed when they are touched, and
  by a sweep of the whole map, which runs inside a write when at least one minute has passed
  since the previous sweep. There is no goroutine and nothing to close.
- It is safe for concurrent use: one mutex guards the map, and `Incr` is atomic under it.
- It suits tests and single-instance deployments. State is lost on restart, which logs every
  user out.

## 5. Reference adapter: Redis (README)

This is the README's example, which uses `github.com/redis/go-redis/v9`. The module does not
depend on it.

```go
// RedisStore adapts a go-redis client to auth.Store.
type RedisStore struct{ C redis.UniversalClient }

func (s RedisStore) Get(ctx context.Context, key string) ([]byte, error) {
	b, err := s.C.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, auth.ErrNotFound
	}
	return b, err
}

func (s RedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.C.Set(ctx, key, value, ttl).Err()
}

func (s RedisStore) Delete(ctx context.Context, key string) error {
	return s.C.Del(ctx, key).Err()
}

// incr sets the expiry only when the counter is created, atomically.
var incr = redis.NewScript(`local n = redis.call("INCR", KEYS[1])
if n == 1 then redis.call("PEXPIRE", KEYS[1], ARGV[1]) end
return n`)

func (s RedisStore) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	return incr.Run(ctx, s.C, []string{key}, ttl.Milliseconds()).Int64()
}
```

Notes for the README:

- `INCR` followed by `PEXPIRE` inside one script is atomic: Redis runs a script without
  interleaving other commands. Two separate commands would leave a counter without expiry if
  the client died between them.
- Each operation touches one key, so the adapter works unchanged with Redis Cluster.
- `Set` with a TTL maps to `SET key value PX ttl`, and `Get` of an expired key returns
  `redis.Nil`.
