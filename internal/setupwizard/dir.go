package setupwizard

import "os"

// defaultDir is /out when the image is run with a mount there (the documented
// docker run command), else the current directory.
func defaultDir() string {
	if fi, err := os.Stat("/out"); err == nil && fi.IsDir() {
		return "/out"
	}
	return "."
}
