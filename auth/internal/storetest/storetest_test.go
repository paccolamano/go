package storetest_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/internal/storetest"
	"github.com/paccolamano/go/auth/memstore"
)

var (
	_ auth.Store = (*storetest.Recorder)(nil)
	_ auth.Store = storetest.Failing{}
)

func TestRecorder(t *testing.T) {
	ctx := t.Context()
	r := &storetest.Recorder{Next: memstore.New()}
	if err := r.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Get(ctx, "k"); err != nil || string(got) != "v" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if _, err := r.Incr(ctx, "c", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	calls := r.Calls()
	want := []storetest.Call{
		{Op: storetest.OpSet, Key: "k", Value: []byte("v")},
		{Op: storetest.OpGet, Key: "k"},
		{Op: storetest.OpIncr, Key: "c"},
		{Op: storetest.OpDelete, Key: "k"},
	}
	if len(calls) != len(want) {
		t.Fatalf("Calls = %v", calls)
	}
	for i := range want {
		if calls[i].Op != want[i].Op || calls[i].Key != want[i].Key || string(calls[i].Value) != string(want[i].Value) {
			t.Fatalf("call %d = %+v, want %+v", i, calls[i], want[i])
		}
	}
	calls[0].Key = "mutated"
	if r.Calls()[0].Key != "k" {
		t.Fatal("Calls does not return a copy")
	}
	if r.Count(storetest.OpGet) != 1 || r.Count(storetest.OpSet) != 1 {
		t.Fatalf("Count = %d gets, %d sets", r.Count(storetest.OpGet), r.Count(storetest.OpSet))
	}
	r.Reset()
	if len(r.Calls()) != 0 {
		t.Fatal("Reset did not clear")
	}
	if _, err := r.Get(ctx, "absent"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("Get(absent) = %v, want ErrNotFound", err)
	}
}

func TestFailing(t *testing.T) {
	ctx := t.Context()
	boom := errors.New("boom")
	f := storetest.Failing{Next: memstore.New(), Err: boom, Ops: []storetest.Op{storetest.OpIncr, storetest.OpGet}}
	if _, err := f.Incr(ctx, "c", time.Minute); !errors.Is(err, boom) {
		t.Fatalf("Incr = %v, want boom", err)
	}
	if _, err := f.Get(ctx, "k"); !errors.Is(err, boom) {
		t.Fatalf("Get = %v, want boom", err)
	}
	if err := f.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Set = %v, want forwarded", err)
	}
	if err := f.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete = %v, want forwarded", err)
	}
}

func TestConcurrent(t *testing.T) {
	ctx := t.Context()
	r := &storetest.Recorder{Next: memstore.New()}
	f := storetest.Failing{Next: r, Err: errors.New("x"), Ops: []storetest.Op{storetest.OpDelete}}
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			_, _ = f.Incr(ctx, "c", time.Minute)
			_ = r.Count(storetest.OpIncr)
			_ = r.Calls()
		})
	}
	wg.Wait()
	if n := r.Count(storetest.OpIncr); n != 50 {
		t.Fatalf("Count(Incr) = %d, want 50", n)
	}
}
