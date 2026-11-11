package middleware

import (
	"errors"
	"net/http"
	"testing"
)

// TestChain_UseErrorSkipsMiddleware checks that a constructor result with an
// error adds no middleware, even when the constructor also returned one, and
// leaves the receiver unchanged (FR-003, US1 scenario 2).
func TestChain_UseErrorSkipsMiddleware(t *testing.T) {
	identity := func(h http.Handler) http.Handler { return h }
	errA := errors.New("a failed")
	base := New(identity)
	for name, mw := range map[string]Middleware{"no middleware": nil, "a middleware too": identity} {
		t.Run(name, func(t *testing.T) {
			c := base.Use(mw, errA)
			if len(c.mws) != 1 {
				t.Errorf("len(mws) = %d, want 1: the middleware was added", len(c.mws))
			}
			if len(c.errs) != 1 || !errors.Is(c.errs[0], errA) {
				t.Errorf("errs = %v, want [%v]", c.errs, errA)
			}
			if len(base.mws) != 1 || len(base.errs) != 0 {
				t.Errorf("Use modified the receiver: mws %d, errs %v", len(base.mws), base.errs)
			}
		})
	}
}
