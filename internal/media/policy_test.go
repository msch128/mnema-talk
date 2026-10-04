package media

import (
	"strings"
	"testing"
)

var (
	pngBytes  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")
	htmlBytes = []byte("<!DOCTYPE html><html><script>alert(document.cookie)</script></html>")
	svgBytes  = []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	textBytes = []byte("just some notes\nline two\n")
)

func TestReconcileMIME(t *testing.T) {
	cases := []struct {
		name     string
		declared string
		data     []byte
		want     string
		wantErr  bool
	}{
		{"real png", "image/png", pngBytes, "image/png", false},
		{"png declared as octet-stream is recognised", "application/octet-stream", pngBytes, "image/png", false},
		{"html disguised as png", "image/png", htmlBytes, "", true},
		{"html declared honestly", "text/html", htmlBytes, "", true},
		{"html declared as text", "text/plain", htmlBytes, "", true},
		{"svg is accepted (download-only)", "image/svg+xml", svgBytes, "image/svg+xml", false},
		{"plain text", "text/plain", textBytes, "text/plain", false},
		{"text claiming pdf", "application/pdf", textBytes, "", true},
		{"unlisted declared type", "application/x-msdownload", textBytes, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reconcileMIME(tc.declared, tc.data, allowedMIME, false)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("mime = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAvatarRequiresRealImage(t *testing.T) {
	if _, err := reconcileMIME("image/svg+xml", svgBytes, avatarMIME, true); err == nil {
		t.Fatal("svg accepted as avatar")
	}
	if _, err := reconcileMIME("image/png", htmlBytes, avatarMIME, true); err == nil {
		t.Fatal("html accepted as avatar")
	}
	if got, err := reconcileMIME("image/png", pngBytes, avatarMIME, true); err != nil || got != "image/png" {
		t.Fatalf("png avatar: %q %v", got, err)
	}
}

func TestDangerousTypesAreNeverInline(t *testing.T) {
	for mt := range inlineMIME {
		if strings.HasPrefix(mt, "text/") || strings.Contains(mt, "svg") || strings.Contains(mt, "html") || mt == "application/pdf" {
			t.Errorf("%s must not be served inline", mt)
		}
	}
	for mt := range inlineMIME {
		if !allowedMIME[mt] {
			t.Errorf("inline type %s is not in the upload allow-list", mt)
		}
	}
}

func TestDeclaredMIMEFallsBackToExtension(t *testing.T) {
	if got := declaredMIME("", "clip.mp4"); got != "video/mp4" {
		t.Errorf("got %q", got)
	}
	if got := declaredMIME("image/PNG; charset=binary", "x"); got != "image/png" {
		t.Errorf("got %q", got)
	}
	if got := declaredMIME("", "noext"); got != "application/octet-stream" {
		t.Errorf("got %q", got)
	}
}
