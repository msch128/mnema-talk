package ws

import (
	"bytes"
	"encoding/json"
	"io"
)

// nativeAccessTokenLen is the length of a base64url-encoded 32-byte access token.
const nativeAccessTokenLen = 43

// parseNativeRenewFrame strictly decodes {"type":"native_access_renew",
// "payload":{"access_token":"..."}} and returns the token. Anything else
// (unknown or duplicate keys, extra fields, trailing data) is rejected.
//
// Only the trusted native broker constructs this control. Never forward its
// payload to application handlers, diagnostics, event broadcasts or a renderer.
func parseNativeRenewFrame(raw []byte) (string, bool) {
	if len(raw) > 1024 {
		return "", false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if !expectToken(d, json.Delim('{')) {
		return "", false
	}
	var frame renewFrame
	for d.More() {
		if !frame.readMember(d) {
			return "", false
		}
	}
	if !expectToken(d, json.Delim('}')) || !frame.seenType || !frame.seenPayload {
		return "", false
	}
	if _, err := d.Token(); err != io.EOF {
		return "", false
	}
	return frame.token, true
}

// renewFrame collects the members of a renew frame as they are read.
type renewFrame struct {
	seenType, seenPayload bool
	token                 string
}

// readMember reads one "key": value member; any unknown or repeated key, or
// a malformed value, is refused.
func (f *renewFrame) readMember(d *json.Decoder) bool {
	key, err := d.Token()
	if err != nil {
		return false
	}
	switch key {
	case "type":
		if f.seenType || !expectToken(d, "native_access_renew") {
			return false
		}
		f.seenType = true
	case "payload":
		if f.seenPayload {
			return false
		}
		f.seenPayload = true
		var ok bool
		f.token, ok = readRenewPayload(d)
		return ok
	default:
		return false
	}
	return true
}

// readRenewPayload reads {"access_token":"<token>"} and nothing more.
func readRenewPayload(d *json.Decoder) (string, bool) {
	if !expectToken(d, json.Delim('{')) || !expectToken(d, "access_token") {
		return "", false
	}
	t, err := d.Token()
	if err != nil {
		return "", false
	}
	value, ok := t.(string)
	if !ok || len(value) != nativeAccessTokenLen {
		return "", false
	}
	if d.More() || !expectToken(d, json.Delim('}')) {
		return "", false
	}
	return value, true
}

// expectToken reads the next token and reports whether it is exactly want.
func expectToken(d *json.Decoder, want json.Token) bool {
	t, err := d.Token()
	return err == nil && t == want
}
