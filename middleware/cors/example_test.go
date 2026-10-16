package cors_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/paccolamano/go/middleware/cors"
)

func ExampleNew() {
	mw, err := cors.New(
		[]string{"https://app.example.com", "https://*.example.org"},
		cors.WithMethods(http.MethodGet, http.MethodPut),
		cors.WithHeaders("Content-Type", "Authorization"),
		cors.WithCredentials(),
	)
	if err != nil {
		panic(err)
	}
	h := mw(http.NotFoundHandler())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/items", nil)
	req.Header.Set("Origin", "https://admin.example.org")
	req.Header.Set("Access-Control-Request-Method", http.MethodPut)
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Access-Control-Allow-Origin"))
	fmt.Println(rec.Header().Get("Access-Control-Allow-Methods"))
	// Output:
	// 204
	// https://admin.example.org
	// GET, PUT
}
