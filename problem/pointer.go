package problem

import "strings"

// fragmentPunctuation lists the characters other than letters and digits that
// a reference token keeps as they are in a URI fragment (RFC 3986 §3.5): the
// unreserved marks except '~', the sub-delimiters, ':', '@' and '?'. Pointer
// escapes '~' and '/' itself.
const fragmentPunctuation = "-._!$&'()*+,;=:@?"

// Pointer returns the JSON Pointer (RFC 6901), in URI fragment form, to the
// member reached through the given reference tokens: Pointer("items", "0",
// "qty") is "#/items/0/qty", and Pointer() is "#", the whole document.
//
// In each token, "~" becomes "~0" and "/" becomes "~1" (RFC 6901 §3). Then
// every byte that a URI fragment does not allow, including "%" and the bytes of
// non-ASCII characters, is percent-encoded (RFC 6901 §6): Pointer("città") is
// "#/citt%C3%A0". Tokens are not interpreted: pass an array index as decimal
// text.
func Pointer(tokens ...string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.WriteByte('#')
	for _, tok := range tokens {
		b.WriteByte('/')
		for i := range len(tok) {
			switch c := tok[i]; {
			case c == '~':
				b.WriteString("~0")
			case c == '/':
				b.WriteString("~1")
			case isLetter(c), isDigit(c), strings.IndexByte(fragmentPunctuation, c) >= 0:
				b.WriteByte(c)
			default:
				b.WriteByte('%')
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0x0f])
			}
		}
	}
	return b.String()
}
