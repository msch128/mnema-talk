package media

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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

// open fetches the object from offset now, before any response header is
// written, so a missing object becomes an error response instead of a 200
// with a truncated body. A later Seek to the same offset keeps the body.
func (o *objectReader) open(offset int64) error {
	_ = o.Close()
	body, err := o.store.GetObjectFrom(o.ctx, o.key, offset)
	if err != nil {
		return err
	}
	o.off, o.body = offset, body
	return nil
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

// rangeStart is where http.ServeContent will start reading for a Range
// header: the start of a single byte range, else 0. Anything unusual (several
// ranges, a start past the end) yields 0; ServeContent handles those itself.
func rangeStart(header string, size int64) int64 {
	spec, ok := strings.CutPrefix(strings.TrimSpace(header), "bytes=")
	if !ok || strings.Contains(spec, ",") {
		return 0
	}
	first, last, ok := strings.Cut(strings.TrimSpace(spec), "-")
	if !ok {
		return 0
	}
	first, last = strings.TrimSpace(first), strings.TrimSpace(last)
	if first == "" {
		// Suffix range: the last n bytes.
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n <= 0 {
			return 0
		}
		if n >= size {
			return 0
		}
		return size - n
	}
	start, err := strconv.ParseInt(first, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0
	}
	return start
}

// isMissingObject reports whether err means the stored object does not exist.
func isMissingObject(err error) bool {
	var noKey *types.NoSuchKey
	var notFound *types.NotFound
	return errors.Is(err, ErrObjectNotFound) || errors.As(err, &noKey) || errors.As(err, &notFound)
}
