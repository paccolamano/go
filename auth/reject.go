package auth

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/paccolamano/go/auth/internal/authheader"
)

// plainChallenges returns each leaf's challenge for ErrNoCredentials, in leaf
// order, without empty values or duplicates.
func plainChallenges(ls []Authenticator) []string {
	var out []string
	for _, l := range ls {
		out = appendChallenge(out, challenge(l, ErrNoCredentials))
	}
	return out
}

// invalidChallenges returns the challenges for invalid credentials rejected by
// leaf i: its challenge for ErrInvalidCredentials replaces its plain challenge
// and every other challenge of the same scheme.
func invalidChallenges(ls []Authenticator, i int) []string {
	special := challenge(ls[i], ErrInvalidCredentials)
	if special == "" {
		return plainChallenges(ls)
	}
	scheme := authheader.Scheme(special)
	var out []string
	for j, l := range ls {
		if j == i {
			out = appendChallenge(out, special)
			continue
		}
		if c := challenge(l, ErrNoCredentials); authheader.Scheme(c) != scheme {
			out = appendChallenge(out, c)
		}
	}
	return out
}

func challenge(a Authenticator, cause error) string {
	if c, ok := a.(Challenger); ok {
		return c.Challenge(cause)
	}
	return ""
}

func appendChallenge(out []string, c string) []string {
	if c == "" || slices.Contains(out, c) {
		return out
	}
	return append(out, c)
}

// writeRejection sets one WWW-Authenticate field line per challenge, then
// writes the response with fn, or the default plain-text response when fn is
// nil. When fn writes nothing, the status is sent with an empty body.
func writeRejection(w http.ResponseWriter, r *http.Request, fn RejectFunc, rej Rejection, challenges []string) {
	for _, c := range challenges {
		w.Header().Add("WWW-Authenticate", c)
	}
	if fn == nil {
		http.Error(w, http.StatusText(rej.Status), rej.Status)
		return
	}
	tw := &trackingWriter{ResponseWriter: w}
	fn(tw, r, rej)
	if !tw.started {
		w.WriteHeader(rej.Status)
	}
}

// trackingWriter records whether a RejectFunc wrote a status or a body.
type trackingWriter struct {
	http.ResponseWriter
	started bool
}

func (t *trackingWriter) WriteHeader(code int) {
	t.started = true
	t.ResponseWriter.WriteHeader(code)
}

func (t *trackingWriter) Write(p []byte) (int, error) {
	t.started = true
	n, err := t.ResponseWriter.Write(p)
	if err != nil {
		return n, fmt.Errorf("auth: write rejection: %w", err)
	}
	return n, nil
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (t *trackingWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }
