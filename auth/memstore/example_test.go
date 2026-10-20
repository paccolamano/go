package memstore_test

import (
	"context"
	"fmt"
	"time"

	"github.com/paccolamano/go/auth/memstore"
)

func ExampleNew() {
	ctx := context.Background()
	s := memstore.New()

	_ = s.Set(ctx, "greeting", []byte("hello"), time.Minute)
	v, _ := s.Get(ctx, "greeting")
	fmt.Println(string(v))

	for range 3 {
		n, _ := s.Incr(ctx, "attempts", time.Minute)
		fmt.Println(n)
	}
	// Output:
	// hello
	// 1
	// 2
	// 3
}
