package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/accesslog"
	"github.com/paccolamano/go/middleware/bodylimit"
	"github.com/paccolamano/go/middleware/compress"
	"github.com/paccolamano/go/middleware/contenttype"
	"github.com/paccolamano/go/middleware/cors"
	"github.com/paccolamano/go/middleware/decompress"
	"github.com/paccolamano/go/middleware/etag"
	"github.com/paccolamano/go/middleware/maxconcurrency"
	"github.com/paccolamano/go/middleware/ratelimit"
	"github.com/paccolamano/go/middleware/realip"
	"github.com/paccolamano/go/middleware/recovery"
	"github.com/paccolamano/go/middleware/requestid"
	"github.com/paccolamano/go/middleware/secureheaders"
)

// jsonReject writes every refusal of the chain as a JSON document.
func jsonReject(w http.ResponseWriter, r *http.Request, rej middleware.Rejection) {
	code := "error"
	switch {
	case errors.Is(rej.Err, ratelimit.ErrLimitExceeded):
		code = "rate_limited"
	case errors.Is(rej.Err, contenttype.ErrUnsupported):
		code = "unsupported_media_type"
	}
	id, _ := requestid.FromContext(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rej.Status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "request_id": id})
}

// recommendedChain builds every middleware of the module in the recommended
// order, with the same refusal format everywhere.
func recommendedChain(logger *slog.Logger) (middleware.Chain, error) {
	var errs []error
	add := func(mw middleware.Middleware, err error) middleware.Middleware {
		errs = append(errs, err)
		return mw
	}
	chain := middleware.New(
		add(realip.New(realip.TrustRanges("10.0.0.0/8"))),
		add(requestid.New()),
		add(accesslog.New(logger)),
		add(recovery.New(logger, recovery.WithRejectFunc(jsonReject))),
		add(secureheaders.New()),
		add(cors.New([]string{"https://app.example.com"})),
		add(ratelimit.New(100, time.Minute, ratelimit.WithRejectFunc(jsonReject))),
		add(maxconcurrency.New(256, maxconcurrency.WithRejectFunc(jsonReject))),
		add(bodylimit.New(1<<20, bodylimit.WithRejectFunc(jsonReject))),
		add(decompress.New(decompress.WithRejectFunc(jsonReject))),
		add(contenttype.New([]string{"application/json"}, contenttype.WithRejectFunc(jsonReject))),
		add(compress.New()),
		add(etag.New()),
	)
	return chain, errors.Join(errs...)
}

func ExampleNew_recommended() {
	logger := slog.New(slog.DiscardHandler)
	chain, err := recommendedChain(logger)
	if err != nil {
		panic(err)
	}
	h := chain.Then(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[`+strings.Repeat(`"item",`, 300)+`"last"]}`)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/items", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	fmt.Println("status:", rec.Code)
	fmt.Println("encoding:", rec.Header().Get("Content-Encoding"))
	fmt.Println("weak etag:", strings.HasPrefix(rec.Header().Get("ETag"), `W/"`))
	fmt.Println("request id:", rec.Header().Get("X-Request-ID") != "")
	fmt.Println("csp:", rec.Header().Get("Content-Security-Policy"))

	refused := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/items", strings.NewReader("x"))
	refused.Header.Set("Content-Type", "text/plain")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, refused)
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	fmt.Println("refusal:", rec.Code, body["code"])
	// Output:
	// status: 200
	// encoding: gzip
	// weak etag: true
	// request id: true
	// csp: default-src 'none'; frame-ancestors 'none'
	// refusal: 415 unsupported_media_type
}
