//go:build vetcheck

package errors_test

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// TestVetReportsWrapfMisuse checks that go vet's printf analyzer accepts %w in
// Errorf and reports it in Wrapf (FR-010). It spawns the go toolchain, so it
// runs only with -tags vetcheck.
func TestVetReportsWrapfMisuse(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go toolchain not found: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), goTool, "vet", "./testdata/vetmisuse")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("go vet reported nothing:\n%s", out)
	}
	findings := regexp.MustCompile(`(?m)^.*vetmisuse\.go:\d+:\d+: .*$`).FindAllString(string(out), -1)
	want := []string{
		"errors.Wrapf does not support error-wrapping directive %w",
		"reads arg #1, but call has 0 args",
		"(github.com/paccolamano/go/errors.Skip).Wrapf does not support error-wrapping directive %w",
	}
	if len(findings) != len(want) {
		t.Fatalf("got %d findings, want %d:\n%s", len(findings), len(want), out)
	}
	for _, w := range want {
		found := false
		for _, f := range findings {
			found = found || strings.Contains(f, w)
		}
		if !found {
			t.Errorf("missing finding %q in:\n%s", w, out)
		}
	}
}
