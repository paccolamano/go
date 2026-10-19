package slogx_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/paccolamano/go/slogx"
	"github.com/paccolamano/go/slogx/internal/attrs"
)

func TestPanic_ContextFunc(t *testing.T) {
	panicking := func(context.Context) []slog.Attr { panic("boom") }
	fine := func(context.Context) []slog.Attr { return []slog.Attr{slog.String("k", "v")} }
	var buf bytes.Buffer
	contextLogger(t, &buf, slogx.WithContextFunc(panicking), slogx.WithContextFunc(fine)).InfoContext(context.Background(), "m")
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"m","!CONTEXTFUNC":"panic: boom","k":"v"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestNil_ContextHandler(t *testing.T) {
	var nilCtx context.Context
	var buf bytes.Buffer
	h := must(slogx.NewContextHandler(newJSON(&buf, nil), slogx.WithContextFunc(alwaysAttr)))
	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "m", 0)
	for _, hh := range []slog.Handler{h, h.WithGroup("g")} {
		if err := hh.Handle(nilCtx, r); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(lines(t, &buf)); n != 2 {
		t.Errorf("got %d records, want 2", n)
	}
	h2 := must(slogx.NewContextHandler(newJSON(&buf, nil)))
	if err := h2.Handle(nilCtx, r); err != nil {
		t.Fatal(err)
	}
}

// panickingValuer panics when it is resolved.
type panickingValuer struct{}

func (panickingValuer) LogValue() slog.Value { panic("boom") }

// selfValuer resolves to a group that contains itself.
type selfValuer struct{}

func (s selfValuer) LogValue() slog.Value { return slog.GroupValue(slog.Any("x", s)) }

func TestPanic_LogValue(t *testing.T) {
	var buf bytes.Buffer
	redactLogger(&buf).Info("m", "v", panickingValuer{}, "after", 1)
	rec := decode(t, onlyLine(t, &buf))
	if v, _ := rec["v"].(string); !strings.HasPrefix(v, "LogValue panicked") || rec["after"] != float64(1) {
		t.Errorf("record = %v, want the slog placeholder for v and the other attributes", rec)
	}
}

func TestDepth_SelfReferencingValuer(t *testing.T) {
	var buf bytes.Buffer
	redactLogger(&buf).Info("m", "v", selfValuer{})
	line := onlyLine(t, &buf)
	if got := strings.Count(line, `":{`); got != attrs.MaxDepth {
		t.Errorf("nesting = %d group levels, want %d", got, attrs.MaxDepth)
	}
	if !strings.Contains(line, `"x":"`+attrs.DepthPlaceholder+`"`) {
		t.Errorf("record does not end with the depth placeholder: %s", line)
	}
}
