package sse

import "testing"

// The vectors follow the Unicode Standard, chapter 3, "U+FFFD Substitution of
// Maximal Subparts" (Table 3-8), which the WHATWG Encoding Standard's UTF-8
// decoder implements.
func TestUTF8_MaximalSubparts(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"table 3-8", []byte{0x61, 0xF1, 0x80, 0x80, 0xE1, 0x80, 0xC2, 0x62, 0x80, 0x63, 0x80, 0xBF, 0x64}, "a���b�c��d"},
		{"overlong two-byte", []byte{0xC0, 0xAF}, "��"},
		{"overlong three-byte", []byte{0xE0, 0x80, 0xAF}, "���"},
		{"surrogate", []byte{0xED, 0xA0, 0x80}, "���"},
		{"above U+10FFFF", []byte{0xF4, 0x90, 0x80, 0x80}, "����"},
		{"truncated at end", []byte{0xE2, 0x82}, "�"},
		{"truncated before ASCII", []byte{0xE2, 0x82, 0x41}, "�A"},
		{"truncated four-byte", []byte{0xF0, 0x9F, 0x98, 0x41}, "�A"},
		{"lone continuation", []byte{0x80}, "�"},
		{"invalid lead bytes", []byte{0xF5, 0xFF}, "��"},
		{"valid", []byte("héllo, 世界 😀"), "héllo, 世界 😀"},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(appendValidUTF8(nil, tt.in)); got != tt.want {
				t.Fatalf("appendValidUTF8(% x) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
