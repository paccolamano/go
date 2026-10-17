package errors_test

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"log"
	"log/slog"
	"strings"
	"testing"

	"github.com/paccolamano/go/errors"
)

func dropTime(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

func jsonLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level, ReplaceAttr: dropTime}))
}

func textLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{ReplaceAttr: dropTime}))
}

func decodeRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("decoding %q: %v", buf.String(), err)
	}
	return record
}

func sampleChain() error {
	var lines []int
	return handleRequest(&lines)
}

func TestAttr_JSONShape(t *testing.T) {
	chain := sampleChain()
	var buf bytes.Buffer
	jsonLogger(&buf, slog.LevelInfo).Error("request failed", errors.Attr(chain))
	attr, ok := decodeRecord(t, &buf)["error"].(map[string]any)
	if !ok || len(attr) != 2 || attr["message"] != chain.Error() {
		t.Fatalf("error attribute = %#v", attr)
	}
	want, _ := json.Marshal(errors.Describe(chain))
	got, _ := json.Marshal(attr["detail"])
	var wantAny, gotAny any
	_ = json.Unmarshal(want, &wantAny)
	_ = json.Unmarshal(got, &gotAny)
	if string(mustJSON(t, gotAny)) != string(mustJSON(t, wantAny)) {
		t.Errorf("detail = %s, want %s", got, want)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAttr_TextHandler(t *testing.T) {
	var buf bytes.Buffer
	textLogger(&buf).Error("request failed", errors.Attr(sampleChain()))
	out := buf.String()
	if !strings.Contains(out, `error.message="handle request: load tenant 42`) ||
		!strings.Contains(out, `error.detail="handle request\n    at `) {
		t.Errorf("text record = %s", out)
	}
}

func TestDetailValue_CustomKey(t *testing.T) {
	chain := sampleChain()
	var buf bytes.Buffer
	jsonLogger(&buf, slog.LevelInfo).Error("request failed", slog.Any("err", errors.DetailValue(chain)))
	if attr, ok := decodeRecord(t, &buf)["err"].(map[string]any); !ok || attr["message"] != chain.Error() || attr["detail"] == nil {
		t.Errorf("err attribute = %#v", attr)
	}
	if key := errors.Attr(chain).Key; key != "error" {
		t.Errorf("default key = %q", key)
	}
}

func TestAttr_Nil(t *testing.T) {
	var buf bytes.Buffer
	jsonLogger(&buf, slog.LevelInfo).Info("ok", errors.Attr(nil), slog.Any("err", errors.DetailValue(nil)))
	record := decodeRecord(t, &buf)
	if _, ok := record["error"]; ok {
		t.Error("Attr(nil) added an attribute")
	}
	if _, ok := record["err"]; ok {
		t.Error("DetailValue(nil) added an attribute")
	}
}

type countingError struct{ calls *int }

func (e countingError) Error() string {
	*e.calls++
	return "counted"
}

func TestAttr_Lazy(t *testing.T) {
	var calls int
	var buf bytes.Buffer
	logger := jsonLogger(&buf, slog.LevelWarn)
	logger.Info("disabled", errors.Attr(countingError{&calls}))
	if calls != 0 || buf.Len() != 0 {
		t.Errorf("a disabled record computed the detail: %d calls, output %q", calls, buf.String())
	}
	logger.Warn("enabled", errors.Attr(countingError{&calls}))
	if calls == 0 {
		t.Error("an enabled record did not compute the detail")
	}
}

func plainSubjects() map[string]error {
	a, b := stderrors.New("a"), stderrors.New("b")
	return map[string]error{
		"wrap":     errors.Wrap(errors.Wrap(a, "inner"), "outer"),
		"join":     errors.Join(a, b),
		"errorf":   errors.Errorf("%w and %w", a, b),
		"custom":   NewConnError("db", a),
		"override": NewOverrideError(7),
	}
}

// TestSlog_PlainValues checks that an error logged without the helper looks
// exactly like a standard library error with the same message (FR-030).
func TestSlog_PlainValues(t *testing.T) {
	for name, err := range plainSubjects() {
		std := stderrors.New(err.Error())
		for handler, logger := range map[string]func(*bytes.Buffer) *slog.Logger{
			"json": func(b *bytes.Buffer) *slog.Logger { return jsonLogger(b, slog.LevelInfo) },
			"text": textLogger,
		} {
			var ours, theirs bytes.Buffer
			logger(&ours).Info("m", "err", err)
			logger(&theirs).Info("m", "err", std)
			if ours.String() != theirs.String() {
				t.Errorf("%s/%s: %s, want %s", name, handler, ours.String(), theirs.String())
			}
		}
	}

	// The default handler formats values through the log package. This part
	// changes process-wide state, restored by t.Cleanup, so it must not run in
	// parallel with other tests.
	var buf bytes.Buffer
	writer, flags := log.Writer(), log.Flags()
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
	log.SetOutput(&buf)
	log.SetFlags(0)
	for name, err := range plainSubjects() {
		buf.Reset()
		slog.Info("m", "err", err)
		ours := buf.String()
		buf.Reset()
		slog.Info("m", "err", stderrors.New(err.Error()))
		if ours != buf.String() {
			t.Errorf("%s/default: %s, want %s", name, ours, buf.String())
		}
	}
}

// inspectingHandler records whether an attribute value, once resolved, still
// leads to the expected errors.
type inspectingHandler struct {
	slog.Handler
	foundRoot, foundConn *bool
	root                 error
}

func (h inspectingHandler) Handle(ctx context.Context, r slog.Record) error {
	r.Attrs(func(a slog.Attr) bool {
		if err, ok := a.Value.Resolve().Any().(error); ok {
			*h.foundRoot = *h.foundRoot || stderrors.Is(err, h.root)
			var ce *ConnError
			*h.foundConn = *h.foundConn || stderrors.As(err, &ce)
		}
		return true
	})
	return h.Handler.Handle(ctx, r)
}

func TestSlog_PlainViewInspectable(t *testing.T) {
	root := stderrors.New("root")
	var foundRoot, foundConn bool
	var buf bytes.Buffer
	logger := slog.New(inspectingHandler{slog.NewJSONHandler(&buf, nil), &foundRoot, &foundConn, root})
	logger.Info("m", "err", errors.Wrap(NewConnError("db", root), "outer"))
	if !foundRoot || !foundConn {
		t.Errorf("resolved value inspection: Is(root) = %v, As(*ConnError) = %v", foundRoot, foundConn)
	}
}
