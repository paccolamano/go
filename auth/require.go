package auth

import (
	"net/http"
	"slices"
)

// RequireMethods returns a middleware that admits only requests whose
// principal's Methods include every one of methods, for example "mfa" on
// sensitive routes. It must run after the middleware returned by New, whose
// RejectFunc and challenges it reuses:
//   - a request without a principal (WithAnonymous) is rejected with 401 and
//     the methods' challenges;
//   - a principal that lacks a method is rejected with ErrInsufficientMethods:
//     401 with the step-up challenge of the method that authenticated it, when
//     that method defines one (tokens, RFC 9470), and 403 otherwise;
//   - used without New, it answers every request with 500.
//
// An empty list only requires a principal; an empty value never matches.
func RequireMethods(methods ...string) func(http.Handler) http.Handler {
	required := slices.Clone(methods)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			st, ok := r.Context().Value(stateKey{}).(*state)
			if !ok {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			if st.principal == nil {
				writeRejection(w, r, st.reject, Rejection{Status: http.StatusUnauthorized, Err: ErrNoCredentials}, st.challenges)
				return
			}
			for _, m := range required {
				if m != "" && slices.Contains(st.principal.Methods, m) {
					continue
				}
				if c := challenge(st.decider, ErrInsufficientMethods); c != "" {
					writeRejection(w, r, st.reject, Rejection{Status: http.StatusUnauthorized, Err: ErrInsufficientMethods}, []string{c})
					return
				}
				writeRejection(w, r, st.reject, Rejection{Status: http.StatusForbidden, Err: ErrInsufficientMethods}, nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
