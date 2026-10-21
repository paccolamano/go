package sse_test

import (
	"fmt"
	"strings"

	"github.com/paccolamano/go/sse"
)

func ExampleDecode() {
	stream := "event: add\nid: 7\ndata: 73857293\n\n: a comment\ndata: two\ndata: lines\n\n"
	for ev, err := range sse.Decode(strings.NewReader(stream)) {
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Printf("type=%q id=%q data=%q\n", ev.Type, ev.ID, ev.Data)
	}
	// Output:
	// type="add" id="7" data="73857293"
	// type="" id="7" data="two\nlines"
}
