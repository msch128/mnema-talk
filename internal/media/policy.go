// Package media handles uploads (chat attachments, avatars), authenticated
// media serving, the admin storage dashboard and the retention worker.
package media

import (
	"mime"
	"net/http"
	"strings"
)

// allowedMIME is the attachment allow-list; anything else is rejected with 415.
//
// SECURITY: image/svg+xml and text/* are dangerous if rendered in the app
// origin. They are only ever served with Content-Disposition: attachment plus
// nosniff and a sandbox CSP (see inlineMIME), so the browser never executes them.
var allowedMIME = map[string]bool{
	"image/png":                   true,
	"image/jpeg":                  true,
	"image/gif":                   true,
	"image/webp":                  true,
	"image/svg+xml":               true, // download-only
	"video/mp4":                   true,
	"video/webm":                  true,
	"audio/mpeg":                  true,
	"audio/wave":                  true,
	"audio/ogg":                   true,
	"application/ogg":             true,
	"application/pdf":             true,
	"text/plain":                  true,
	"text/markdown":               true,
	"text/csv":                    true,
	"application/json":            true,
	"application/zip":             true,
	"application/x-7z-compressed": true,
	"application/octet-stream":    true,
}

// avatarMIME is the tighter allow-list for profile pictures.
var avatarMIME = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// inlineMIME may be served with Content-Disposition: inline. SVG, HTML-ish
// text and documents are deliberately excluded.
var inlineMIME = map[string]bool{
	"image/png":       true,
	"image/jpeg":      true,
	"image/gif":       true,
	"image/webp":      true,
	"video/mp4":       true,
	"video/webm":      true,
	"audio/mpeg":      true,
	"audio/wave":      true,
	"audio/ogg":       true,
	"application/ogg": true,
}

// concreteSniff are types http.DetectContentType recognises by reliable magic
// bytes. When the bytes sniff to one of them, the sniff is authoritative.
var concreteSniff = map[string]bool{
	"image/png":                   true,
	"image/jpeg":                  true,
	"image/gif":                   true,
	"image/webp":                  true,
	"video/mp4":                   true,
	"video/webm":                  true,
	"audio/mpeg":                  true,
	"audio/wave":                  true,
	"application/ogg":             true,
	"application/pdf":             true,
	"application/zip":             true,
	"application/x-7z-compressed": true,
}

// mustMatchWhenDeclared: if the client declares one of these renderable types
// but the bytes do not sniff to it, the upload is a content-confusion attack
// (e.g. HTML declared as image/png) and is rejected.
var mustMatchWhenDeclared = map[string]bool{
	"image/png":       true,
	"image/jpeg":      true,
	"image/gif":       true,
	"image/webp":      true,
	"video/mp4":       true,
	"video/webm":      true,
	"audio/mpeg":      true,
	"audio/wave":      true,
	"application/pdf": true,
}

// indeterminateSniff is what DetectContentType returns when it cannot identify
// the bytes; consistent with text/json/svg/octet-stream declarations.
var indeterminateSniff = map[string]bool{
	"application/octet-stream": true,
	"text/plain":               true,
	"text/xml":                 true,
}

// normalizeMIME strips parameters and lowercases ("text/plain; charset=utf-8" → "text/plain").
func normalizeMIME(ct string) string {
	if mt, _, err := mime.ParseMediaType(ct); err == nil {
		return strings.ToLower(mt)
	}
	return strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
}

// declaredMIME derives the client's claim from the part header, falling back to
// the file extension when the header is missing or generic.
func declaredMIME(header, filename string) string {
	ct := normalizeMIME(header)
	if ct == "" || ct == "application/octet-stream" {
		if i := strings.LastIndexByte(filename, '.'); i >= 0 {
			if byExt := normalizeMIME(mime.TypeByExtension(strings.ToLower(filename[i:]))); byExt != "" {
				return byExt
			}
		}
		return "application/octet-stream"
	}
	return ct
}

type mimeError struct{ msg string }

func (e *mimeError) Error() string { return e.msg }

// reconcileMIME decides the content type to STORE and SERVE. The sniffed type,
// never the client header, is authoritative. Adopted from mnema.xyz.
func reconcileMIME(declared string, head []byte, allow map[string]bool, imageOnly bool) (string, error) {
	sniffed := normalizeMIME(http.DetectContentType(head))

	if concreteSniff[sniffed] {
		if !allow[sniffed] {
			return "", &mimeError{"file type " + sniffed + " is not allowed"}
		}
		return sniffed, nil
	}
	if imageOnly {
		return "", &mimeError{"avatar must be a png, jpeg, gif or webp image"}
	}
	if mustMatchWhenDeclared[declared] {
		return "", &mimeError{"declared " + declared + " but the file content does not match"}
	}
	// text/html and any other surprising sniff are rejected outright.
	if !indeterminateSniff[sniffed] {
		return "", &mimeError{"file type " + sniffed + " is not allowed"}
	}
	if !allow[declared] {
		return "", &mimeError{"file type " + declared + " is not allowed"}
	}
	return declared, nil
}

// extensionFor picks the storage key extension from the final MIME type.
func extensionFor(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/svg+xml":
		return ".svg"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wave":
		return ".wav"
	case "text/plain":
		return ".txt"
	case "text/markdown":
		return ".md"
	}
	if exts, err := mime.ExtensionsByType(mimeType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}
