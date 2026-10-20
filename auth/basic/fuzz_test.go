package basic_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/paccolamano/go/auth"
)

func FuzzBasicHeader(f *testing.F) {
	f.Add(basicHeader("alice", "hunter2"))
	f.Add("Basic !!!")
	f.Add("basic " + base64.StdEncoding.EncodeToString([]byte(":x")))
	f.Add("Bearer x")
	f.Add("")
	f.Fuzz(func(t *testing.T, v string) {
		stub := &stubChecker{p: &auth.Principal{Subject: "x"}}
		_, _ = mustBasic(t, stub).Authenticate(request(t, v))
		if len(stub.calls) == 0 {
			return
		}
		scheme, rest, _ := strings.Cut(v, " ")
		b, err := base64.StdEncoding.DecodeString(strings.TrimLeft(rest, " "))
		user, _, found := strings.Cut(string(b), ":")
		if !strings.EqualFold(scheme, "Basic") || err != nil || !found || user == "" {
			t.Fatalf("checker called for %q", v)
		}
	})
}
