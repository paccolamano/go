package health

import (
	"io"
	"net/http"
	"strconv"
	"strings"
)

// render returns the status and the body of a response (contracts/health-http.md).
func render(checks []check, outcomes []outcome, verbose bool) (int, string) {
	allPassed := true
	for _, o := range outcomes {
		if o.kind != passed {
			allPassed = false
			break
		}
	}
	if allPassed && !verbose {
		return http.StatusOK, "ok\n"
	}
	var b strings.Builder
	for i, c := range checks {
		if outcomes[i].kind == passed {
			b.WriteString("[+]" + c.name + " ok\n")
		} else {
			b.WriteString("[-]" + c.name + " failed: reason withheld\n")
		}
	}
	if allPassed {
		b.WriteString("ok\n")
		return http.StatusOK, b.String()
	}
	b.WriteString("failed\n")
	return http.StatusServiceUnavailable, b.String()
}

// writeText writes a plain text response. A HEAD request gets the headers only.
// The body is made of check names, which the developer defines, and fixed words.
func writeText(w http.ResponseWriter, r *http.Request, status int, body string) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	// A failed write means the prober has gone: there is nothing left to answer.
	_, _ = io.WriteString(w, body)
}
