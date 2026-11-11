package media

import (
	"context"
	"io"

	"github.com/msch128/mnema-talk/internal/imgstrip"
)

// stripBufferBytes is the largest WebP rewritten in memory to remove its
// metadata: what a profile picture may weigh. JPEG and PNG stream, and so
// does a larger WebP (see imgstrip).
const stripBufferBytes = MaxAvatarBytes

// storedBody is what of an upload goes to object storage. n counts it: the
// size that is recorded, served and counted against the quotas is the stored
// one. It is never larger than what was sent (the filter only leaves bytes
// out or overwrites them), so a file within the upload limit is stored within
// it, and smaller when metadata was removed.
type storedBody struct {
	r io.Reader
	n int64
}

func (s *storedBody) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	s.n += int64(n)
	return n, err
}

// stripsMetadata reports the instance setting; without a permission service
// (tests) the default holds.
func (h *Handler) stripsMetadata(ctx context.Context) bool {
	return h.Perms == nil || h.Perms.StripImageMetadata(ctx)
}

// toStore returns the bytes to store for an upload of mimeType: a JPEG, PNG
// or WebP without its metadata and without anything that follows the picture,
// unless the Owner switched that off; every other file as it came.
func (h *Handler) toStore(ctx context.Context, mimeType string, upload io.Reader) *storedBody {
	if !h.stripsMetadata(ctx) {
		return &storedBody{r: upload}
	}
	return &storedBody{r: imgstrip.Reader(mimeType, upload, stripBufferBytes)}
}
