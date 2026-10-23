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
)
