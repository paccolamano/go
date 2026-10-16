package netaddr

import (
	"net/netip"
	"testing"
)

func TestParseRemoteAddr(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"1.2.3.4:5", "1.2.3.4", true},
		{"[::1]:80", "::1", true},
		{"[fe80::1%eth0]:1", "fe80::1", true},
		{"[::ffff:1.2.3.4]:9", "1.2.3.4", true},
		{"10.0.0.1", "10.0.0.1", true},
		{"2001:db8::1", "2001:db8::1", true},
		{"", "", false},
		{"@", "", false},
		{"example.com:80", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := ParseRemoteAddr(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ParseRemoteAddr(%q) ok = %v, want %v", tt.in, ok, tt.wantOK)
			}
			if ok && got.String() != tt.want {
				t.Errorf("ParseRemoteAddr(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestKey(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"192.0.2.10", "192.0.2.10"},
		{"2001:db8:1:2:3:4:5:6", "2001:db8:1:2::/64"},
		{"2001:db8:1:2:ffff::1", "2001:db8:1:2::/64"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := Key(netip.MustParseAddr(tt.in)); got != tt.want {
				t.Errorf("Key(%s) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
	if Key(netip.MustParseAddr("2001:db8:1:2::1")) == Key(netip.MustParseAddr("2001:db8:1:3::1")) {
		t.Error("addresses in different /64 prefixes share a key")
	}
}
