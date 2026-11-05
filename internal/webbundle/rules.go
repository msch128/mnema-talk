package webbundle

import (
	"fmt"
	"path"
	"strings"
)

// omitFromSignedBundle leaves the server-only API reference out of the
// desktop bundle. Vite names that entry's chunks apiDocs-<hash>.
func omitFromSignedBundle(rel string) bool {
	base := path.Base(rel)
	return rel == "api-docs.html" || strings.HasPrefix(base, "apiDocs-")
}

// rejectDotfile stops hidden files and directories (.gitkeep, .well-known/x,
// .vite/manifest.json) from being signed. Every path segment is checked, not
// just the file name: a dotted directory is as hidden as a dotted file.
func rejectDotfile(rel string) error {
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") {
			return fmt.Errorf("dotfile forbidden in signed web bundle: %s", rel)
		}
	}
	return nil
}

// signedExtensions are the file types the web build emits. Anything else in
// the build output is a stray file that must not ride along under the release
// signature, so the build fails closed and a new asset type is added here
// deliberately.
var signedExtensions = map[string]bool{
	".html": true, ".js": true, ".css": true, ".json": true, ".webmanifest": true,
	".svg": true, ".png": true, ".woff": true, ".woff2": true, ".wasm": true,
	".tgz": true, ".txt": true,
}

// rejectUnknownType allows the extensions above, plus the extensionless or
// oddly suffixed LICENSE and NOTICE texts the build ships under licenses/.
func rejectUnknownType(rel string) error {
	base := path.Base(rel)
	if signedExtensions[strings.ToLower(path.Ext(base))] {
		return nil
	}
	if strings.HasPrefix(rel, "licenses/") && (strings.HasPrefix(base, "LICENSE") || strings.HasPrefix(base, "NOTICE")) {
		return nil
	}
	return fmt.Errorf("file type not allowed in signed web bundle: %s", rel)
}
