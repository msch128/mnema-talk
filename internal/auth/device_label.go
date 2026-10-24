package auth

import "strings"

// DeviceLabel turns a User-Agent header into a coarse label such as
// "Firefox · Linux". Only the browser family and the operating system are
// kept: the raw header (versions, build numbers, device models) is never
// stored, so the label cannot fingerprint a device. The separator is neutral
// so the label reads the same in every language. An unrecognised or empty
// header yields "", which the web app shows as "Unknown device".
func DeviceLabel(userAgent string) string {
	ua := strings.ToLower(userAgent)
	browser, system := browserOf(ua), systemOf(ua)
	switch {
	case browser == "":
		return system
	case system == "":
		return browser
	}
	return browser + " · " + system
}

// browserOf names the browser family. Order matters: most browsers also
// announce Chrome or Safari, so the more specific tokens come first.
func browserOf(ua string) string {
	for _, b := range []struct{ token, name string }{
		{"electron/", "Mnema desktop app"},
		{"edg/", "Edge"}, {"edga/", "Edge"}, {"edgios/", "Edge"},
		{"opr/", "Opera"}, {"opera", "Opera"},
		{"vivaldi", "Vivaldi"}, {"samsungbrowser", "Samsung Internet"},
		{"firefox/", "Firefox"}, {"fxios/", "Firefox"},
		{"crios/", "Chrome"}, {"chrome/", "Chrome"}, {"chromium/", "Chromium"},
		{"safari/", "Safari"},
	} {
		if strings.Contains(ua, b.token) {
			return b.name
		}
	}
	return ""
}

// systemOf names the operating system. iOS and Android come before the
// desktop systems they imitate ("like Mac OS X", "Linux; Android").
func systemOf(ua string) string {
	for _, s := range []struct{ token, name string }{
		{"iphone", "iOS"}, {"ipad", "iPadOS"}, {"android", "Android"},
		{"windows", "Windows"}, {"cros", "ChromeOS"},
		{"mac os x", "macOS"}, {"macintosh", "macOS"},
		{"linux", "Linux"}, {"x11", "Linux"},
	} {
		if strings.Contains(ua, s.token) {
			return s.name
		}
	}
	return ""
}
