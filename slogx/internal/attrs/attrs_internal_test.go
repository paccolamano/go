package attrs

import (
	"log/slog"
	"testing"
)

func TestFlatten_Table(t *testing.T) {
	a := slog.String("a", "1")
	b := slog.Int("b", 2)
	c := slog.Bool("c", true)
	emptyGroup := slog.Group("g")
	tests := []struct {
		name string
		dst  []slog.Attr
		in   []slog.Attr
		want []slog.Attr
	}{
		{name: "nil", in: nil, want: nil},
		{name: "plain attributes keep their order", in: []slog.Attr{b, a, c}, want: []slog.Attr{b, a, c}},
		{name: "zero attribute dropped", in: []slog.Attr{a, {}, b}, want: []slog.Attr{a, b}},
		{name: "empty key with a value dropped", in: []slog.Attr{slog.String("", "x"), a}, want: []slog.Attr{a}},
		{name: "empty-key group inlined in place", in: []slog.Attr{c, slog.Group("", a, b), c}, want: []slog.Attr{c, a, b, c}},
		{name: "nested empty-key groups inlined", in: []slog.Attr{slog.Group("", a, slog.Group("", b))}, want: []slog.Attr{a, b}},
		{name: "named group kept unchanged", in: []slog.Attr{slog.Group("g", a, slog.Group("", b))}, want: []slog.Attr{slog.Group("g", a, slog.Group("", b))}},
		{name: "empty named group kept", in: []slog.Attr{emptyGroup}, want: []slog.Attr{emptyGroup}},
		{name: "appends to dst", dst: []slog.Attr{c}, in: []slog.Attr{a}, want: []slog.Attr{c, a}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Flatten(tt.dst, tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("Flatten = %v, want %v", got, tt.want)
			}
			for i := range got {
				if !got[i].Equal(tt.want[i]) {
					t.Errorf("Flatten[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestConstants(t *testing.T) {
	if MaxDepth != 64 {
		t.Errorf("MaxDepth = %d, want 64", MaxDepth)
	}
	if DepthPlaceholder != "!DEPTH" {
		t.Errorf("DepthPlaceholder = %q, want %q", DepthPlaceholder, "!DEPTH")
	}
}
