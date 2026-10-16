package decompress

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

// lazyReader creates the decoder on the first Read, so that a malformed
// header is reported to the handler as a read error, like corrupt data found
// later in the body.
type lazyReader struct {
	src       io.ReadCloser
	newReader func(r io.Reader) (io.ReadCloser, error)
	dec       io.ReadCloser
	err       error
}

func (l *lazyReader) Read(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	if l.dec == nil {
		dec, err := l.newReader(l.src)
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF // an empty body is not a valid encoding
			}
			l.err = wrap(err)
			return 0, l.err
		}
		l.dec = dec
	}
	n, err := l.dec.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = wrap(err)
	}
	return n, err
}

func (l *lazyReader) Close() error {
	var decErr error
	if l.dec != nil {
		decErr = l.dec.Close()
	}
	if err := errors.Join(decErr, l.src.Close()); err != nil {
		return fmt.Errorf("decompress: close: %w", err)
	}
	return nil
}

// wrap marks decoding failures with ErrInvalidBody, except for the "body too
// large" condition of an outer bodylimit, which keeps its meaning.
func wrap(err error) error {
	if mbe := new(http.MaxBytesError); errors.As(err, &mbe) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrInvalidBody, err)
}
