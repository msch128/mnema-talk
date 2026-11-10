package perm

import "context"

// UploadSettings are the global upload settings as the Owner sees them.
type UploadSettings struct {
	// MaxUploadMB is the effective global limit.
	MaxUploadMB int `json:"max_upload_mb" minimum:"1"`
	// CeilingMB is the operator's hard ceiling (MAX_UPLOAD_SIZE_MB).
	CeilingMB int `json:"ceiling_mb" minimum:"1"`
	// StripImageMetadata says whether the server removes metadata (EXIF with
	// the GPS position, XMP, IPTC, comments, thumbnails) and anything that
	// follows the picture from uploaded JPEG, PNG and WebP attachments
	// (default on). A server that does not send it is older and
	// keeps the metadata.
	StripImageMetadata bool `json:"strip_image_metadata" binding:"optional"`
}

// UploadSettings returns the effective global limit, the ceiling and the
// metadata switch.
func (s *Service) UploadSettings(ctx context.Context) (UploadSettings, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return UploadSettings{}, err
	}
	c := s.UploadCeilingMB()
	return UploadSettings{MaxUploadMB: snap.GlobalUploadMB(c), CeilingMB: c, StripImageMetadata: snap.StripImageMetadata()}, nil
}

// StripImageMetadata reports whether uploaded pictures lose their metadata.
// It says yes when the settings cannot be read: an upload that goes through
// then is stored without, never with.
func (s *Service) StripImageMetadata(ctx context.Context) bool {
	snap, err := s.Snapshot(ctx)
	return err != nil || snap.StripImageMetadata()
}
