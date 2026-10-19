package problem_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"

	"github.com/paccolamano/go/problem"
)

// ValidationError is the problem type of a request whose body is not valid.
var ValidationError = problem.Must(problem.NewType(
	"https://example.net/validation-error",
	"Your request is not valid.",
	http.StatusUnprocessableEntity,
))

// Profile is part of the body of a CreateUser request.
type Profile struct{ Color string }

// CreateUser is the body of a request that creates a user.
type CreateUser struct {
	Age     int
	Profile Profile
}

// validate returns a ValidationError problem that lists every invalid member
// of req, or nil when req is valid.
func validate(req CreateUser) error {
	var items []problem.Item
	if req.Age <= 0 {
		items = append(items, problem.Item{Detail: "must be a positive integer", Pointer: problem.Pointer("age")})
	}
	if !slices.Contains([]string{"green", "red", "blue"}, req.Profile.Color) {
		items = append(items, problem.Item{
			Detail:  "must be 'green', 'red' or 'blue'",
			Pointer: problem.Pointer("profile", "color"),
		})
	}
	if len(items) > 0 {
		return ValidationError.New(problem.WithErrors(items...))
	}
	return nil
}

func ExampleWithErrors() {
	err := validate(CreateUser{Age: -1, Profile: Profile{Color: "pink"}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/users", nil)
	if werr := problem.Write(rec, req, err); werr != nil {
		slog.ErrorContext(req.Context(), "write problem", "err", werr)
	}
	fmt.Println(rec.Code)
	fmt.Println(rec.Body)
	// Output:
	// 422
	// {"type":"https://example.net/validation-error","title":"Your request is not valid.","status":422,"errors":[{"detail":"must be a positive integer","pointer":"#/age"},{"detail":"must be 'green', 'red' or 'blue'","pointer":"#/profile/color"}]}
}
