package slogx_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/paccolamano/go/slogx"
)

const (
	secretValue = "hunter2"
	secretHex   = "68756e74657232"
)

type withSecrets struct {
	Exported   slogx.Secret[string]
	unexported slogx.Secret[string]
}

func assertNoSecret(t *testing.T, what, out string) {
	t.Helper()
	if strings.Contains(out, secretValue) || strings.Contains(strings.ToLower(out), secretHex) {
		t.Errorf("%s leaks the secret: %q", what, out)
	}
}

func TestSecret_Fmt(t *testing.T) {
	s := slogx.NewSecret(secretValue)
	v := withSecrets{Exported: s, unexported: s}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%10v", "%-10s"} {
		t.Run(verb, func(t *testing.T) {
			if got := fmt.Sprintf(verb, s); got != "[REDACTED]" {
				t.Errorf("Sprintf(%q, secret) = %q, want [REDACTED]", verb, got)
			}
			assertNoSecret(t, "struct", fmt.Sprintf(verb, v))
			assertNoSecret(t, "pointer to struct", fmt.Sprintf(verb, &v))
			assertNoSecret(t, "pointer", fmt.Sprintf(verb, &s))
		})
	}
	if got := fmt.Sprint(s); got != "[REDACTED]" {
		t.Errorf("Sprint = %q", got)
	}
}

func TestSecret_JSON(t *testing.T) {
	type nested struct{ s slogx.Secret[string] }
	got, err := json.Marshal(struct {
		S slogx.Secret[string]
		N nested
		M map[string]slogx.Secret[int]
	}{S: slogx.NewSecret(secretValue), N: nested{slogx.NewSecret(secretValue)}, M: map[string]slogx.Secret[int]{"pin": slogx.NewSecret(1234)}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"S":"[REDACTED]","N":{},"M":{"pin":"[REDACTED]"}}`; string(got) != want {
		t.Errorf("json = %s, want %s", got, want)
	}
}

func TestSecret_Log(t *testing.T) {
	s := slogx.NewSecret(secretValue)
	var jsonBuf, textBuf bytes.Buffer
	for _, h := range []slog.Handler{newJSON(&jsonBuf, nil), slog.NewTextHandler(&textBuf, &slog.HandlerOptions{ReplaceAttr: dropTime})} {
		slog.New(h).Info("m", "s", s, "v", withSecrets{Exported: s, unexported: s}, "p", &s)
	}
	if got, want := onlyLine(t, &jsonBuf), `{"level":"INFO","msg":"m","s":"[REDACTED]","v":{"Exported":"[REDACTED]"},"p":"[REDACTED]"}`; got != want {
		t.Errorf("json record = %s\nwant          %s", got, want)
	}
	text := onlyLine(t, &textBuf)
	if !strings.HasPrefix(text, `level=INFO msg=m s=[REDACTED] v="{Exported:[REDACTED] unexported:{p:0x`) {
		t.Errorf("text record = %s", text)
	}
	assertNoSecret(t, "text handler", text)
}

func TestSecret_Reveal(t *testing.T) {
	if got := slogx.NewSecret(secretValue).Reveal(); got != secretValue {
		t.Errorf("Reveal = %q, want %q", got, secretValue)
	}
	var zero slogx.Secret[int]
	if got := zero.Reveal(); got != 0 {
		t.Errorf("zero Reveal = %d, want 0", got)
	}
	for name, s := range map[string]interface {
		fmt.Stringer
		slog.LogValuer
		MarshalText() ([]byte, error)
	}{"value": slogx.NewSecret(secretValue), "zero": zero} {
		text, err := s.MarshalText()
		if s.String() != "[REDACTED]" || s.LogValue().String() != "[REDACTED]" || string(text) != "[REDACTED]" || err != nil {
			t.Errorf("%s: String=%q LogValue=%q MarshalText=%q, %v", name, s.String(), s.LogValue().String(), text, err)
		}
	}
}

func TestSecret_ThroughRedactHandler(t *testing.T) {
	var buf bytes.Buffer
	redactLogger(&buf).Info("m", "pin", slogx.NewSecret(1234))
	if got, want := onlyLine(t, &buf), `{"level":"INFO","msg":"m","pin":"[REDACTED]"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}
