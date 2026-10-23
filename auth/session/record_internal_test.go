package session

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"
)

func TestRecord_Fields(t *testing.T) {
	for name, c := range map[string]struct {
		v    any
		keys []string
	}{
		"session":                    {sessionRecord{V: 1, Sub: "u", AMR: []string{"pwd"}, IAT: 1, EXP: 2, RW: 3}, []string{"amr", "exp", "iat", "rw", "sub", "v"}},
		"pending":                    {pendingRecord{V: 1, Sub: "u", AMR: []string{"pwd"}, IAT: 1}, []string{"amr", "iat", "sub", "v"}},
		"session after a revocation": {sessionRecord{V: 1, Sub: "u", AMR: []string{"pwd"}, IAT: 1, EXP: 2, RW: 3, Gen: "g"}, []string{"amr", "exp", "gen", "iat", "rw", "sub", "v"}},
		"pending after a revocation": {pendingRecord{V: 1, Sub: "u", AMR: []string{"pwd"}, IAT: 1, Gen: "g"}, []string{"amr", "gen", "iat", "sub", "v"}},
	} {
		b, err := json.Marshal(c.v)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if got := slices.Sorted(maps.Keys(m)); !slices.Equal(got, c.keys) {
			t.Errorf("%s fields = %v, want %v", name, got, c.keys)
		}
	}
}

func TestRecord_RoundTrip(t *testing.T) {
	s := sessionRecord{V: 1, Sub: "u", AMR: []string{"pwd", "otp", "mfa"}, IAT: 10, EXP: 20, RW: 15}
	b, _ := encodeRecord(s)
	var got sessionRecord
	if err := decodeRecord(b, &got); err != nil || got.Sub != s.Sub || !slices.Equal(got.AMR, s.AMR) || got.IAT != 10 || got.EXP != 20 || got.RW != 15 {
		t.Fatalf("decode = %+v, %v", got, err)
	}
	p := pendingRecord{V: 1, Sub: "u", AMR: []string{"pwd"}, IAT: 10}
	b, _ = encodeRecord(p)
	var gotP pendingRecord
	if err := decodeRecord(b, &gotP); err != nil || gotP.Sub != "u" || gotP.IAT != 10 {
		t.Fatalf("decode pending = %+v, %v", gotP, err)
	}
}

func TestRecord_Invalid(t *testing.T) {
	for _, b := range []string{`{"v":2,"sub":"u"}`, `{"sub":"u"}`, `not json`, ``} {
		var r sessionRecord
		if err := decodeRecord([]byte(b), &r); err == nil {
			t.Errorf("decodeRecord(%q) succeeded", b)
		}
	}
}

// TestRecord_PreviousRelease checks both directions of compatibility with
// auth/v0.2.0, whose decodeRecord is the same function and whose records have
// no gen field.
func TestRecord_PreviousRelease(t *testing.T) {
	type sessionV020 struct {
		V   int      `json:"v"`
		Sub string   `json:"sub"`
		AMR []string `json:"amr"`
		IAT int64    `json:"iat"`
		EXP int64    `json:"exp"`
		RW  int64    `json:"rw"`
	}
	b, _ := encodeRecord(sessionRecord{V: 1, Sub: "u", AMR: []string{"pwd"}, IAT: 1, EXP: 2, RW: 3, Gen: "g"})
	var old sessionV020
	if err := decodeRecord(b, &old); err != nil || old.Sub != "u" || old.EXP != 2 || old.RW != 3 {
		t.Fatalf("v0.2.0 decoding a record of this release = %+v, %v", old, err)
	}

	const v020 = `{"v":1,"sub":"u","amr":["pwd"],"iat":1,"exp":2,"rw":3}`
	var cur sessionRecord
	if err := decodeRecord([]byte(v020), &cur); err != nil || cur.Gen != "" || cur.Sub != "u" {
		t.Fatalf("decoding a v0.2.0 record = %+v, %v", cur, err)
	}
	unrevoked, _ := encodeRecord(sessionRecord{V: 1, Sub: "u", AMR: []string{"pwd"}, IAT: 1, EXP: 2, RW: 3})
	if string(unrevoked) != v020 {
		t.Fatalf("record of a never revoked subject = %s, want the v0.2.0 bytes %s", unrevoked, v020)
	}
}
