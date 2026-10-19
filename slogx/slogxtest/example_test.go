package slogxtest_test

import (
	"fmt"
	"log/slog"

	"github.com/paccolamano/go/slogx/slogxtest"
)

// A test checks what the code under test logged: the message, the level and
// the attributes, including those added to the logger and those in groups.
func ExampleRecorder() {
	rec := slogxtest.NewRecorder()
	logger := slog.New(rec)

	// Code under test.
	logger.With("request_id", "abc").Error("panic recovered", slog.Group("http", "method", "GET"))

	r, ok := rec.Last()
	if !ok {
		panic("nothing was logged")
	}
	id, _ := r.Attr("request_id")
	method, _ := r.Attr("http", "method")
	fmt.Println(r.Level, r.Message, id, method)
	// Output:
	// ERROR panic recovered abc GET
}
