package jwt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	minLifetime     = time.Minute
	maxLifetime     = 24 * time.Hour
	defaultLifetime = time.Hour
	maxDocument     = 1 << 20
	downloadTimeout = 10 * time.Second
)

// keySet is a remote JWK Set, downloaded lazily and cached. It starts no
// goroutine: downloads run in the request that triggers them, and concurrent
// requests share one download.
type keySet struct {
	url     string
	client  *http.Client
	refresh time.Duration

	mu          sync.Mutex
	keys        []boundKey
	expires     time.Time
	lastAttempt time.Time
	lastErr     error
	inflight    chan struct{}
}

// find returns the keys that may verify a token with kid and alg. An error
// means that the decision depends on a key set that could not be downloaded.
func (s *keySet) find(ctx context.Context, kid, alg string) ([]boundKey, error) {
	for {
		s.mu.Lock()
		now := time.Now()
		fresh := len(s.keys) > 0 && now.Before(s.expires)
		found := candidates(s.keys, kid, alg)
		allowed := s.lastAttempt.IsZero() || now.Sub(s.lastAttempt) >= s.refresh

		switch {
		case fresh && len(found) > 0:
			s.mu.Unlock()
			return found, nil
		case s.inflight != nil:
			ch := s.inflight
			s.mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return nil, fmt.Errorf("jwt: wait for key set: %w", ctx.Err())
			}
		case allowed && (!fresh || kid != ""):
			ch := make(chan struct{})
			s.inflight = ch
			s.lastAttempt = now
			s.mu.Unlock()

			keys, lifetime, err := s.download(ctx)

			s.mu.Lock()
			if err == nil {
				s.keys, s.expires, s.lastErr = keys, time.Now().Add(lifetime), nil
			} else {
				s.lastErr = err
			}
			s.inflight = nil
			close(ch)
			s.mu.Unlock()
			continue
		default:
			lastErr := s.lastErr
			s.mu.Unlock()
			if len(found) > 0 {
				return found, nil // keys downloaded earlier stay in use
			}
			return nil, lastErr
		}
	}
}

// download fetches and parses the key set. It detaches from the caller's
// cancellation, so that one client going away does not fail the requests
// waiting for the same download, and bounds it with its own timeout.
func (s *keySet) download(ctx context.Context) (keys []boundKey, lifetime time.Duration, err error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("jwt: key set request: %w", err)
	}
	req.Header.Set("Accept", "application/jwk-set+json, application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("jwt: download key set: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("jwt: close key set response: %w", cerr))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("jwt: download key set: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDocument+1))
	if err != nil {
		return nil, 0, fmt.Errorf("jwt: read key set: %w", err)
	}
	if len(body) > maxDocument {
		return nil, 0, errors.New("jwt: the key set exceeds 1 MiB")
	}
	keys, err = parseKeySet(body)
	if err != nil {
		return nil, 0, err
	}
	return keys, cacheLifetime(resp.Header.Get("Cache-Control")), nil
}

// cacheLifetime returns how long a key set may be cached: max-age from
// Cache-Control, the minimum for no-cache and no-store, clamped to between one
// minute and 24 hours, and one hour when the header states no valid max-age.
func cacheLifetime(header string) time.Duration {
	maxAge := int64(-1)
	for _, d := range strings.Split(header, ",") {
		d = strings.ToLower(strings.TrimSpace(d))
		switch {
		case d == "no-cache" || d == "no-store":
			return minLifetime
		case strings.HasPrefix(d, "max-age="):
			if n, err := strconv.ParseInt(strings.TrimPrefix(d, "max-age="), 10, 64); err == nil && n >= 0 {
				maxAge = n
			}
		}
	}
	switch {
	case maxAge < 0:
		return defaultLifetime
	case maxAge > int64(maxLifetime/time.Second):
		return maxLifetime
	}
	return max(time.Duration(maxAge)*time.Second, minLifetime)
}
