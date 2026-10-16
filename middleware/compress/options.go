package compress

import (
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/paccolamano/go/middleware"
	"github.com/paccolamano/go/middleware/internal/httpheader"
	"github.com/paccolamano/go/middleware/internal/mediatype"
)

// Option configures the middleware returned by New.
type Option func(*config)

type coding struct {
	name      string
	newWriter func(w io.Writer) (Writer, error)
}

type config struct {
	level         int
	minSize       int
	contentTypes  []string
	custom        []coding
	customInvalid []string // option errors found while registering codings
	preference    []string
	preferenceSet bool
}

func defaultContentTypes() []string {
	return []string{"text/*", "application/json", "application/*+json", "application/javascript",
		"application/xml", "application/*+xml", "application/wasm", "image/svg+xml"}
}

func defaultConfig() config {
	return config{level: flate.DefaultCompression, minSize: 1024, contentTypes: defaultContentTypes()}
}

// WithLevel sets the compression level of the built-in gzip and deflate
// codings, from -2 (Huffman only) to 9 (best compression). The default is
// flate.DefaultCompression.
func WithLevel(level int) Option {
	return func(c *config) { c.level = level }
}

// WithMinSize sets the size, in bytes, from which a response is compressed.
// The default is 1024. A response that is flushed before reaching it is
// compressed anyway, since it is being streamed.
func WithMinSize(bytes int) Option {
	return func(c *config) { c.minSize = bytes }
}

// WithContentTypes replaces the list of compressible media types. Patterns are
// "type/subtype", "type/*", "type/*+suffix" or "*/*", in lowercase. The
// default is text/*, application/json, application/*+json,
// application/javascript, application/xml, application/*+xml,
// application/wasm and image/svg+xml.
func WithContentTypes(patterns ...string) Option {
	return func(c *config) { c.contentTypes = patterns }
}

// WithCoding registers a content coding, such as "br" or "zstd", implemented
// by a Writer that the caller provides; the module does not depend on any
// compression library beyond the standard one. Registering "gzip" or "deflate"
// replaces the built-in implementation. Custom codings are preferred over the
// built-in ones, in registration order, unless WithPreference says otherwise.
func WithCoding(name string, newWriter func(w io.Writer) (Writer, error)) Option {
	return func(c *config) {
		if !httpheader.ValidToken(name) || name != strings.ToLower(name) || name == "identity" || name == "*" {
			c.customInvalid = append(c.customInvalid, fmt.Sprintf("%q is not a valid lowercase coding name", name))
			return
		}
		if newWriter == nil {
			c.customInvalid = append(c.customInvalid, fmt.Sprintf("the constructor of %q must not be nil", name))
			return
		}
		if i := slices.IndexFunc(c.custom, func(k coding) bool { return k.name == name }); i >= 0 {
			c.custom[i].newWriter = newWriter
			return
		}
		c.custom = append(c.custom, coding{name: name, newWriter: newWriter})
	}
}

// WithPreference sets the codings offered, in order of preference. Codings not
// listed are disabled.
func WithPreference(names ...string) Option {
	return func(c *config) {
		c.preference = names
		c.preferenceSet = true
	}
}

// codings resolves the configuration into the ordered codings to offer and
// the matcher of compressible types.
func (c *config) codings() ([]coding, mediatype.Matcher, error) {
	if len(c.customInvalid) > 0 {
		return nil, mediatype.Matcher{}, invalid("WithCoding", c.customInvalid[0])
	}
	if c.level < flate.HuffmanOnly || c.level > flate.BestCompression {
		return nil, mediatype.Matcher{}, invalid("WithLevel", fmt.Sprintf("%d is outside -2..9", c.level))
	}
	if c.minSize < 0 {
		return nil, mediatype.Matcher{}, invalid("WithMinSize", "must not be negative")
	}
	types, err := mediatype.ParsePatterns(c.contentTypes)
	if err != nil {
		return nil, mediatype.Matcher{}, invalid("WithContentTypes", err.Error())
	}
	level := c.level
	byName := map[string]coding{
		"gzip": {name: "gzip", newWriter: func(w io.Writer) (Writer, error) {
			zw, err := gzip.NewWriterLevel(w, level)
			if err != nil {
				return nil, fmt.Errorf("compress: gzip: %w", err)
			}
			return zw, nil
		}},
		"deflate": {name: "deflate", newWriter: func(w io.Writer) (Writer, error) {
			zw, err := zlib.NewWriterLevel(w, level)
			if err != nil {
				return nil, fmt.Errorf("compress: deflate: %w", err)
			}
			return zw, nil
		}},
	}
	order := make([]string, 0, len(c.custom)+2)
	for _, k := range c.custom {
		if _, builtin := byName[k.name]; !builtin {
			order = append(order, k.name)
		}
		byName[k.name] = k
	}
	order = append(order, "gzip", "deflate")
	if c.preferenceSet {
		if len(c.preference) == 0 {
			return nil, mediatype.Matcher{}, invalid("WithPreference", "at least one coding is required")
		}
		seen := map[string]bool{}
		for _, name := range c.preference {
			if _, ok := byName[name]; !ok {
				return nil, mediatype.Matcher{}, invalid("WithPreference", fmt.Sprintf("unknown coding %q", name))
			}
			if seen[name] {
				return nil, mediatype.Matcher{}, invalid("WithPreference", fmt.Sprintf("duplicate coding %q", name))
			}
			seen[name] = true
		}
		order = c.preference
	}
	codings := make([]coding, 0, len(order))
	for _, name := range order {
		codings = append(codings, byName[name])
	}
	return codings, types, nil
}

func invalid(option, reason string) error {
	return fmt.Errorf("%w: compress: %s: %s", middleware.ErrInvalidConfig, option, reason)
}
