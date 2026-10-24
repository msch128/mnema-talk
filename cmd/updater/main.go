// Command updater is the self-update sidecar: it verifies the signature and
// provenance of a new release image before the app is restarted on it
// (SECURITY.md, "Self-update sidecar").
package main

import (
	"os"

	"github.com/msch128/mnema-talk/internal/updaterd"
)

func main() {
	os.Exit(updaterd.Run(os.Getenv, os.Stderr))
}
