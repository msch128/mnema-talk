package imgproc

// Sizes of profile pictures.
const (
	// BannerMaxWidth and BannerMaxHeight are the largest stored banner (3:1).
	BannerMaxWidth  = 1500
	BannerMaxHeight = 500
	// BannerMinWidth and BannerMinHeight are the smallest source accepted once
	// it is cropped to 3:1 (smaller pictures would be blurry when stretched).
	BannerMinWidth  = 300
	BannerMinHeight = 100
	// AvatarMaxEdge is the largest stored avatar (a square).
	AvatarMaxEdge = 512
	// LegacyMaxEdge and LegacyMaxPixels bound the canvas of an avatar stored
	// before 1.1 that is redrawn once (see ProcessLegacyAvatar).
	LegacyMaxEdge   = 30000
	LegacyMaxPixels = 64 << 20
)

// Sizes of the community icon: both are squares, always exactly this size.
const (
	IconLargeEdge = 512
	IconSmallEdge = 128
	// IconMinEdge is the smallest source accepted; a smaller picture would be
	// blurry at 512.
	IconMinEdge = 128
)
