// Command webbundle signs and verifies the web app bundle for the trusted
// desktop client (doc/desktop-trusted-client.md).
package main

import (
	"os"

	"github.com/mnema-xyz/talk/internal/webbundle"
)

func main() {
	os.Exit(webbundle.Run(os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr))
}
