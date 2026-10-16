package httpheader

import "strings"

// MatchIfNoneMatch reports whether the If-None-Match header values match
// etag, the entity tag of the response, using the weak comparison RFC 9110
// prescribes for If-None-Match. "*" matches any tag. A malformed list never
// matches.
func MatchIfNoneMatch(values []string, etag string) bool {
	opaque, ok := opaqueTag(etag)
	if !ok {
		return false
	}
	for _, v := range values {
		if strings.Trim(v, " \t") == "*" {
			return true
		}
		tags, ok := parseTagList(v)
		if !ok {
			return false
		}
		for _, t := range tags {
			if t == opaque {
				return true
			}
		}
	}
	return false
}

// opaqueTag returns the quoted opaque-tag of an entity tag, without the weak
// indicator.
func opaqueTag(etag string) (string, bool) {
	etag = strings.TrimPrefix(etag, "W/")
	if len(etag) < 2 || etag[0] != '"' || etag[len(etag)-1] != '"' || strings.IndexByte(etag[1:len(etag)-1], '"') >= 0 {
		return "", false
	}
	return etag, true
}

// parseTagList parses a comma-separated list of entity tags and returns their
// quoted opaque-tags. Commas inside a tag are part of it.
func parseTagList(s string) ([]string, bool) {
	var tags []string
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == ' ' || c == '\t' || c == ',':
			i++
			continue
		case strings.HasPrefix(s[i:], "W/"):
			i += 2
		}
		if i >= len(s) || s[i] != '"' {
			return nil, false
		}
		end := strings.IndexByte(s[i+1:], '"')
		if end < 0 {
			return nil, false
		}
		tag := s[i : i+end+2]
		for j := 1; j < len(tag)-1; j++ {
			if c := tag[j]; c < 0x21 || c == 0x7f {
				return nil, false
			}
		}
		tags = append(tags, tag)
		i += end + 2
		// After a tag only OWS and a comma (or the end) may follow.
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i < len(s) && s[i] != ',' {
			return nil, false
		}
	}
	return tags, true
}
