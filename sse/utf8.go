package sse

import "unicode/utf8"

// appendValidUTF8 appends src to dst, replacing each maximal subpart of an
// invalid UTF-8 sequence with U+FFFD, as the WHATWG Encoding Standard's UTF-8
// decoder does. strings.ToValidUTF8 coalesces runs of invalid bytes and
// utf8.DecodeRune replaces them one by one, so neither matches browsers.
func appendValidUTF8(dst, src []byte) []byte {
	for i := 0; i < len(src); {
		if src[i] < utf8.RuneSelf {
			dst = append(dst, src[i])
			i++
			continue
		}
		if r, size := utf8.DecodeRune(src[i:]); r != utf8.RuneError || size > 1 {
			dst = append(dst, src[i:i+size]...)
			i += size
			continue
		}
		dst = utf8.AppendRune(dst, utf8.RuneError)
		i += maximalSubpart(src[i:])
	}
	return dst
}

// maximalSubpart returns the length of the longest prefix of b, which starts
// with an invalid sequence, that is the start of a valid sequence; at least 1.
func maximalSubpart(b []byte) int {
	lo, hi := byte(0x80), byte(0xBF)
	var need int
	switch lead := b[0]; {
	case lead >= 0xC2 && lead <= 0xDF:
		need = 2
	case lead == 0xE0:
		need, lo = 3, 0xA0
	case lead == 0xED:
		need, hi = 3, 0x9F
	case lead >= 0xE1 && lead <= 0xEF:
		need = 3
	case lead == 0xF0:
		need, lo = 4, 0x90
	case lead == 0xF4:
		need, hi = 4, 0x8F
	case lead >= 0xF1 && lead <= 0xF3:
		need = 4
	default:
		return 1
	}
	n := 1
	for n < need && n < len(b) && b[n] >= lo && b[n] <= hi {
		n++
		lo, hi = 0x80, 0xBF
	}
	return n
}
