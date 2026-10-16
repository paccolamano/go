// Package cors implements Cross-Origin Resource Sharing as defined by the
// Fetch standard.
//
// For an allowed origin, responses carry the headers that grant the origin
// access. Requests from other origins still reach the next handler, but their
// responses carry no grant, so browsers do not expose them to scripts.
// Preflight requests (OPTIONS with Origin and Access-Control-Request-Method)
// are answered by the middleware with 204 No Content and never reach the next
// handler, including for paths that do not exist.
//
// When the response depends on the requesting origin, it carries
// Vary: Origin; preflight responses also vary on the requested method and
// headers. With "*", every response carries Access-Control-Allow-Origin: *
// and no Vary: Origin.
//
// Origins are given as "scheme://host[:port]", as "scheme://*.domain[:port]"
// to allow every subdomain (but not the domain itself), as "null", or as a
// single "*" for any origin. "*" cannot be combined with credentials, and the
// "null" origin is allowed only when listed explicitly.
//
// Mandatory settings: the allowed origins; the list may be empty when
// [WithOriginFunc] is set.
//
// Options and defaults:
//   - [WithOriginFunc]: none.
//   - [WithMethods]: GET, HEAD, POST.
//   - [WithHeaders]: only the CORS-safelisted headers.
//   - [WithExposedHeaders]: none.
//   - [WithCredentials]: not allowed.
//   - [WithMaxAge]: 10 minutes.
//
// Recommended position: after secureheaders and before ratelimit and the other
// middleware that refuse requests, so that preflights are answered early and
// refusals such as 429 stay readable by browser scripts.
package cors

import (
	"net/http"
	"strings"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/httpheader"
)

// New returns a middleware that applies the CORS policy for origins.
func New(origins []string, opts ...Option) (middleware.Middleware, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	p, err := newPolicy(origins, cfg)
	if err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if r.Method == http.MethodOptions && origin != "" && r.Header.Get("Access-Control-Request-Method") != "" {
				p.preflight(w, r, origin)
				return
			}
			p.actual(w.Header(), origin)
			next.ServeHTTP(w, r)
		})
	}, nil
}

func (p *policy) actual(h http.Header, origin string) {
	if p.origins.any {
		h.Set("Access-Control-Allow-Origin", "*")
		if p.exposed != "" {
			h.Set("Access-Control-Expose-Headers", p.exposed)
		}
		return
	}
	httpheader.AddVary(h, "Origin")
	if origin == "" || !p.origins.allowed(origin) {
		return
	}
	h.Set("Access-Control-Allow-Origin", origin)
	if p.credentials {
		h.Set("Access-Control-Allow-Credentials", "true")
	}
	if p.exposed != "" {
		h.Set("Access-Control-Expose-Headers", p.exposed)
	}
}

func (p *policy) preflight(w http.ResponseWriter, r *http.Request, origin string) {
	h := w.Header()
	if !p.origins.any {
		httpheader.AddVary(h, "Origin")
	}
	httpheader.AddVary(h, "Access-Control-Request-Method")
	httpheader.AddVary(h, "Access-Control-Request-Headers")

	requested := httpheader.SplitList(r.Header.Values("Access-Control-Request-Headers"))
	if p.origins.allowed(origin) && p.methodAllowed(r.Header.Get("Access-Control-Request-Method")) && p.headersAllowed(requested) {
		if p.origins.any {
			h.Set("Access-Control-Allow-Origin", "*")
		} else {
			h.Set("Access-Control-Allow-Origin", origin)
		}
		if p.credentials {
			h.Set("Access-Control-Allow-Credentials", "true")
		}
		h.Set("Access-Control-Allow-Methods", p.allowMethods)
		if len(requested) > 0 {
			if p.anyHeader {
				h.Set("Access-Control-Allow-Headers", strings.Join(requested, ", "))
			} else {
				h.Set("Access-Control-Allow-Headers", p.allowHeaders)
			}
		}
		if p.maxAge != "" {
			h.Set("Access-Control-Max-Age", p.maxAge)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *policy) methodAllowed(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost:
		return true
	}
	_, ok := p.methods[method]
	return ok
}

func (p *policy) headersAllowed(requested []string) bool {
	if p.anyHeader {
		return true
	}
	for _, h := range requested {
		if _, ok := p.headers[strings.ToLower(h)]; !ok {
			return false
		}
	}
	return true
}
