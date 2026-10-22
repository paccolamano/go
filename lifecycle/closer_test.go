package lifecycle_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/paccolamano/go/lifecycle"
)

func TestCloser_Shapes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var calls []string
		record := func(name string) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, name)
		}
		var hasDeadline bool
		app := mustNew(t)
		app.Add("a", lifecycle.Closer(func() { record("a") }))
		app.Add("b", lifecycle.Closer(func() error { record("b"); return nil }))
		app.Add("c", lifecycle.Closer(func(ctx context.Context) error {
			_, hasDeadline = ctx.Deadline()
			record("c")
			return nil
		}))

		if err := stopAfterLaunch(t, app); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if !slices.Equal(calls, []string{"c", "b", "a"}) {
			t.Fatalf("close calls = %v, want [c b a], once each", calls)
		}
		if !hasDeadline {
			t.Fatal("the context given to the close function has no deadline")
		}
	})
}

func TestCloser_Error(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := mustNew(t)
		app.Add("db", lifecycle.Closer(func() error { return errors.New("close failed") }))

		err := stopAfterLaunch(t, app)
		if err == nil || !strings.Contains(err.Error(), `lifecycle: component "db": stop: close failed`) {
			t.Fatalf("Run = %v, want the stop error of db", err)
		}
	})
}

func TestCloser_Nil(t *testing.T) {
	tests := []struct {
		name string
		c    lifecycle.Component
	}{
		{"func()", lifecycle.Closer((func())(nil))},
		{"func() error", lifecycle.Closer((func() error)(nil))},
		{"func(context.Context) error", lifecycle.Closer((func(context.Context) error)(nil))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := mustNew(t)
			app.Add("c", tt.c)
			err := app.Run(t.Context())
			var ce *lifecycle.ComponentError
			if !errors.Is(err, lifecycle.ErrInvalidConfig) || !errors.As(err, &ce) || ce.Name != "c" {
				t.Fatalf("Run = %v, want ErrInvalidConfig for c", err)
			}
		})
	}
}
