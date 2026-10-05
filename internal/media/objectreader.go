package media

import (
	"context"
	"errors"
	"io"
)

// objectReader is an io.ReadSeeker over a stored object for http.ServeContent.
// Seeking is free; the object is fetched from the current offset on the next
// Read, so a Range request downloads only the requested part from storage.
type objectReader struct {
	ctx   context.Context
	store Store
	key   string
	size  int64

	off  int64
	body io.ReadCloser
}

func (o *objectReader) Read(p []byte) (int, error) {
	if o.off >= o.size {
		return 0, io.EOF
	}
	if o.body == nil {
		body, err := o.store.GetObjectFrom(o.ctx, o.key, o.off)
		if err != nil {
			return 0, err
		}
		o.body = body
	}
	n, err := o.body.Read(p)
	o.off += int64(n)
	return n, err
}

func (o *objectReader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = o.off + offset
	case io.SeekEnd:
		abs = o.size + offset
	default:
		return 0, errors.New("objectReader: invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("objectReader: negative position")
	}
	if abs != o.off {
		_ = o.Close()
		o.off = abs
	}
	return abs, nil
}

func (o *objectReader) Close() error {
	if o.body == nil {
		return nil
	}
	err := o.body.Close()
	o.body = nil
	return err
}
