package password_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/paccolamano/go/auth"
	"github.com/paccolamano/go/auth/memstore"
	"github.com/paccolamano/go/auth/password"
)

// users stands in for the developer's database: username → account.
var users = map[string]password.Account{}

func lookupUser(_ context.Context, username string) (password.Account, error) {
	acct, ok := users[username]
	if !ok {
		return password.Account{}, password.ErrNotFound
	}
	return acct, nil
}

func ExampleHash() {
	hash, err := password.Hash("correct horse battery staple")
	if err != nil {
		panic(err)
	}
	fmt.Println(strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$"))
	// Output: true
}

func ExampleChecker() {
	hash, _ := password.Hash("correct horse battery staple")
	users["alice"] = password.Account{Subject: "user-42", Hash: hash}

	checker, err := password.NewChecker(lookupUser)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()

	p, err := checker.Check(ctx, "alice", "correct horse battery staple")
	fmt.Println(p.Subject, p.Methods, err)

	_, err = checker.Check(ctx, "alice", "wrong")
	fmt.Println(errors.Is(err, auth.ErrInvalidCredentials))

	_, err = checker.Check(ctx, "nobody", "correct horse battery staple")
	fmt.Println(errors.Is(err, auth.ErrInvalidCredentials))
	// Output:
	// user-42 [pwd] <nil>
	// true
	// true
}

func ExampleWithAttemptLimit() {
	hash, _ := password.Hash("correct horse battery staple")
	users["carol"] = password.Account{Subject: "user-7", Hash: hash}

	checker, _ := password.NewChecker(lookupUser,
		password.WithAttemptLimit(password.AttemptLimit{Store: memstore.New(), Max: 3}))
	ctx := context.Background()
	for range 3 {
		_, _ = checker.Check(ctx, "carol", "guess")
	}
	_, err := checker.Check(ctx, "carol", "correct horse battery staple")
	fmt.Println(errors.Is(err, auth.ErrTooManyAttempts))
	// Output: true
}

func ExampleWithRehash() {
	// A hash created by an older system.
	legacy, _ := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), bcrypt.DefaultCost)
	users["dave"] = password.Account{Subject: "user-9", Hash: string(legacy)}

	checker, _ := password.NewChecker(lookupUser, password.WithRehash(func(_ context.Context, subject, hash string) {
		// Store the new hash; handle failures here, the login has succeeded.
		users["dave"] = password.Account{Subject: subject, Hash: hash}
	}))
	_, err := checker.Check(context.Background(), "dave", "correct horse battery staple")
	fmt.Println(err, strings.HasPrefix(users["dave"].Hash, "$argon2id$"))
	// Output: <nil> true
}
