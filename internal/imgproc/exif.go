package imgproc

// TIFFOrientation returns the orientation (1 to 8) that the TIFF block of an
// EXIF record gives a picture, or 1 when it names none or cannot be read. It
// is how JPEG, PNG and WebP all carry it.
func TIFFOrientation(tiff []byte) int { return tiffOrientation(tiff) }
