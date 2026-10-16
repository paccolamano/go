package httpheader

import (
	"strconv"
	"strings"
)

// Negotiate chooses a content coding for a response from the Accept-Encoding
// header values of the request and the server's preference, a list of
// lowercase coding names in order of preference.
//
// Quality values are honored, "x-gzip" counts as "gzip", "*" applies to the
// codings not named explicitly and q=0 excludes a coding; ties are broken by
// the server's preference and entries with a malformed quality are ignored. It
// returns "" (no coding) when the header is absent or nothing in preference is
// acceptable.
func Negotiate(acceptEncoding []string, preference []string) string {
	explicit := map[string]float64{}
	star := -1.0
	for _, item := range SplitList(acceptEncoding) {
		coding, q, ok := parseCoding(item)
		if !ok {
			continue
		}
		if coding == "*" {
			star = q
			continue
		}
		explicit[coding] = q
	}
	best, bestQ := "", 0.0
	for _, coding := range preference {
		q, ok := explicit[coding]
		if !ok {
			q = max(star, 0)
		}
		if q > bestQ {
			best, bestQ = coding, q
		}
	}
	return best
}

// parseCoding parses `coding *( OWS ";" OWS name=value )`, returning the
// lowercase coding and its quality (1 when absent).
func parseCoding(item string) (string, float64, bool) {
	parts := strings.Split(item, ";")
	coding := strings.ToLower(strings.Trim(parts[0], " \t"))
	if coding == "x-gzip" {
		coding = "gzip"
	}
	if coding != "*" && !ValidToken(coding) {
		return "", 0, false
	}
	q := 1.0
	for _, p := range parts[1:] {
		name, value, ok := strings.Cut(strings.Trim(p, " \t"), "=")
		if !ok || !strings.EqualFold(strings.Trim(name, " \t"), "q") {
			continue
		}
		v, err := strconv.ParseFloat(strings.Trim(value, " \t"), 64)
		if err != nil || v < 0 || v > 1 {
			return "", 0, false
		}
		q = v
	}
	return coding, q, true
}
