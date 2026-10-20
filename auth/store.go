package auth

import (
	"context"
	"time"
)

// Store is a key-value store with expiry that holds the library's short-lived
// state: sessions, pending logins and counters. The library chooses every key
// and never stores a secret in clear.
//
// Implementations must guarantee:
//   - Get returns an error matching ErrNotFound when the key is absent or its
//     TTL has elapsed;
//   - Set and Incr receive ttl > 0;
//   - Delete of an absent key returns nil;
//   - Incr creates the counter at 1 with expiry now + ttl and never extends
//     the expiry of an existing counter; N concurrent calls on the same new
//     key return each value from 1 to N exactly once;
//   - every operation honors the cancellation of ctx.
//
// The library never calls Get on a key it increments, nor Incr on a key it
// sets.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
}
