// Package storetest wraps an auth.Store to record or fail its calls, for tests.
package storetest

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/paccolamano/go/auth"
)

// Op names a Store operation.
type Op string

// The Store operations.
const (
	OpGet    Op = "get"
	OpSet    Op = "set"
	OpDelete Op = "delete"
	OpIncr   Op = "incr"
)

// Call is one recorded Store call. Value is set only for OpSet.
type Call struct {
	Op    Op
	Key   string
	Value []byte
}

// Recorder forwards every call to Next and records it. It is safe for
// concurrent use.
type Recorder struct {
	Next auth.Store

	mu    sync.Mutex
	calls []Call
}

func (r *Recorder) record(c Call) {
	r.mu.Lock()
	r.calls = append(r.calls, c)
	r.mu.Unlock()
}

// Calls returns a copy of the recorded calls, in order.
func (r *Recorder) Calls() []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

// Count returns the number of recorded calls of op.
func (r *Recorder) Count(op Op) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if c.Op == op {
			n++
		}
	}
	return n
}

// Reset clears the recorded calls.
func (r *Recorder) Reset() {
	r.mu.Lock()
	r.calls = nil
	r.mu.Unlock()
}

// Get records and forwards the call.
func (r *Recorder) Get(ctx context.Context, key string) ([]byte, error) {
	r.record(Call{Op: OpGet, Key: key})
	b, err := r.Next.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("storetest: %w", err)
	}
	return b, nil
}

// Set records and forwards the call.
func (r *Recorder) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	r.record(Call{Op: OpSet, Key: key, Value: slices.Clone(value)})
	if err := r.Next.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("storetest: %w", err)
	}
	return nil
}

// Delete records and forwards the call.
func (r *Recorder) Delete(ctx context.Context, key string) error {
	r.record(Call{Op: OpDelete, Key: key})
	if err := r.Next.Delete(ctx, key); err != nil {
		return fmt.Errorf("storetest: %w", err)
	}
	return nil
}

// Incr records and forwards the call.
func (r *Recorder) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	r.record(Call{Op: OpIncr, Key: key})
	n, err := r.Next.Incr(ctx, key, ttl)
	if err != nil {
		return 0, fmt.Errorf("storetest: %w", err)
	}
	return n, nil
}

// Failing returns Err for the operations listed in Ops, without calling Next,
// and forwards the others. It is safe for concurrent use.
type Failing struct {
	Next auth.Store
	Err  error
	Ops  []Op
}

func (f Failing) fails(op Op) bool { return slices.Contains(f.Ops, op) }

// Get fails or forwards the call.
func (f Failing) Get(ctx context.Context, key string) ([]byte, error) {
	if f.fails(OpGet) {
		return nil, f.Err
	}
	b, err := f.Next.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("storetest: %w", err)
	}
	return b, nil
}

// Set fails or forwards the call.
func (f Failing) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if f.fails(OpSet) {
		return f.Err
	}
	if err := f.Next.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("storetest: %w", err)
	}
	return nil
}

// Delete fails or forwards the call.
func (f Failing) Delete(ctx context.Context, key string) error {
	if f.fails(OpDelete) {
		return f.Err
	}
	if err := f.Next.Delete(ctx, key); err != nil {
		return fmt.Errorf("storetest: %w", err)
	}
	return nil
}

// Incr fails or forwards the call.
func (f Failing) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if f.fails(OpIncr) {
		return 0, f.Err
	}
	n, err := f.Next.Incr(ctx, key, ttl)
	if err != nil {
		return 0, fmt.Errorf("storetest: %w", err)
	}
	return n, nil
}
