package webbundle

// The desktop clients read the signed documents (manifest.json, trust.json)
// with serde_json and deny_unknown_fields (the Rust host) or a port of that
// reader (the Linux client). encoding/json is laxer: it matches keys in any
// case, takes the last of two equal keys, reads null as the zero value,
// accepts a missing field, replaces invalid UTF-8 and lone surrogates, and
// Decode stops after the first value. strictDocument applies the clients'
// reading first, so Verify and VerifyTrust accept exactly what they accept.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"unicode/utf8"
)

type fieldKind int

const (
	kindString    fieldKind = iota
	kindU32                 // an integer token in 0..2^32-1
	kindU64                 // an integer token in 0..2^64-1
	kindStrings             // an array of strings
	kindStringMap           // an object of strings
	kindFiles               // an array of manifest file records
)

type field struct {
	kind     fieldKind
	optional bool
}

var strictManifest = map[string]field{
	"schema": {kind: kindU32}, "name": {kind: kindString}, "version": {kind: kindString},
	"web_client_api": {kind: kindU32}, "min_server_version": {kind: kindString},
	"security_floor": {kind: kindString}, "commit": {kind: kindString}, "key_id": {kind: kindString},
	"files": {kind: kindFiles},
}

var strictFile = map[string]field{"path": {kind: kindString}, "sha256": {kind: kindString}, "size": {kind: kindU64}}

var strictTrust = map[string]field{
	"schema": {kind: kindU32}, "serial": {kind: kindU64}, "issued_at": {kind: kindString},
	"expires_at": {kind: kindString}, "bundle_keys": {kind: kindStrings},
	"revoked_bundle_keys": {kind: kindStrings}, "updater_keys": {kind: kindStrings},
	"revoked_updater_keys": {kind: kindStrings}, "max_version": {kind: kindStringMap, optional: true},
}

// integerToken is how serde_json writes an unsigned integer: no sign, no
// leading zero, no fraction, no exponent (so -0, 1.0 and 1e0 are not one).
var integerToken = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// strictDocument checks that doc is one JSON object in UTF-8, without a
// byte order mark, duplicate keys or lone surrogates, whose keys are exactly
// the fields given (spelled exactly, the optional ones may be missing) and
// whose values have the field's type, null included in no type.
func strictDocument(doc []byte, fields map[string]field) error {
	if !utf8.Valid(doc) || !json.Valid(doc) {
		return errors.New("not one JSON document in UTF-8")
	}
	if err := checkSurrogates(doc); err != nil {
		return err
	}
	if err := checkDuplicateKeys(doc); err != nil {
		return err
	}
	return checkObject(bytes.TrimSpace(doc), fields)
}

var errLoneSurrogate = errors.New("a lone surrogate in a string")

// checkSurrogates refuses a \u escape of half a surrogate pair, which
// encoding/json would turn into U+FFFD. doc is valid JSON.
func checkSurrogates(doc []byte) error {
	inString := false
	for i := 0; i < len(doc); i++ {
		switch {
		case doc[i] == '"':
			inString = !inString
		case inString && doc[i] == '\\':
			next, err := skipEscape(doc, i)
			if err != nil {
				return err
			}
			i = next
		}
	}
	return nil
}

// skipEscape checks the escape at doc[i] (a backslash inside a string) and
// returns the index of its last byte.
func skipEscape(doc []byte, i int) (int, error) {
	if doc[i+1] != 'u' {
		return i + 1, nil
	}
	unit := hex4(doc[i+2 : i+6])
	switch {
	case unit >= 0xdc00 && unit <= 0xdfff:
		return 0, errLoneSurrogate
	case unit < 0xd800 || unit > 0xdbff:
		return i + 5, nil
	}
	// A high half must be followed by an escaped low half.
	if i+11 >= len(doc) || doc[i+6] != '\\' || doc[i+7] != 'u' {
		return 0, errLoneSurrogate
	}
	if low := hex4(doc[i+8 : i+12]); low < 0xdc00 || low > 0xdfff {
		return 0, errLoneSurrogate
	}
	return i + 11, nil
}

func hex4(b []byte) uint64 {
	n, _ := strconv.ParseUint(string(b), 16, 16) // valid JSON: four hex digits
	return n
}

// keyFrame is an object (keys set) or an array (keys nil) being read.
type keyFrame struct {
	keys    map[string]bool
	wantKey bool
}

// checkDuplicateKeys refuses an object, at any depth, that names a key twice
// (compared after unescaping, as the clients do). doc is valid JSON.
func checkDuplicateKeys(doc []byte) error {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var stack []*keyFrame
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if stack, err = readToken(stack, tok); err != nil {
			return err
		}
	}
}

// readToken applies one token to the stack of open containers.
func readToken(stack []*keyFrame, tok json.Token) ([]*keyFrame, error) {
	var top *keyFrame
	if len(stack) > 0 {
		top = stack[len(stack)-1]
	}
	if s, ok := tok.(string); ok && top != nil && top.keys != nil && top.wantKey {
		if top.keys[s] {
			return nil, fmt.Errorf("duplicate key %q", s)
		}
		top.keys[s], top.wantKey = true, false
		return stack, nil
	}
	switch tok {
	case json.Delim('{'):
		return append(stack, &keyFrame{keys: map[string]bool{}, wantKey: true}), nil
	case json.Delim('['):
		return append(stack, &keyFrame{}), nil
	case json.Delim('}'), json.Delim(']'):
		stack = stack[:len(stack)-1]
	}
	if len(stack) > 0 && stack[len(stack)-1].keys != nil {
		stack[len(stack)-1].wantKey = true // a value ended
	}
	return stack, nil
}

func checkObject(raw json.RawMessage, fields map[string]field) error {
	var obj map[string]json.RawMessage
	if len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, &obj) != nil {
		return errors.New("not a JSON object")
	}
	for name, v := range obj {
		f, ok := fields[name]
		if !ok {
			return fmt.Errorf("unknown field %q", name)
		}
		if err := checkValue(v, f.kind); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	for name, f := range fields {
		if _, ok := obj[name]; !ok && !f.optional {
			return fmt.Errorf("missing field %q", name)
		}
	}
	return nil
}

func checkValue(v json.RawMessage, kind fieldKind) error {
	switch kind {
	case kindString:
		return checkString(v)
	case kindU32:
		return checkUnsigned(v, 32)
	case kindU64:
		return checkUnsigned(v, 64)
	case kindStrings:
		return checkArray(v, checkString)
	case kindFiles:
		return checkArray(v, func(item json.RawMessage) error { return checkObject(item, strictFile) })
	default: // kindStringMap
		var m map[string]json.RawMessage
		if v[0] != '{' || json.Unmarshal(v, &m) != nil {
			return errors.New("not an object")
		}
		for _, item := range m {
			if err := checkString(item); err != nil {
				return err
			}
		}
		return nil
	}
}

func checkString(v json.RawMessage) error {
	if v[0] != '"' {
		return errors.New("not a string")
	}
	return nil
}

func checkUnsigned(v json.RawMessage, bits int) error {
	if _, err := strconv.ParseUint(string(v), 10, bits); err != nil || !integerToken.Match(v) {
		return fmt.Errorf("not an unsigned %d-bit integer", bits)
	}
	return nil
}

func checkArray(v json.RawMessage, item func(json.RawMessage) error) error {
	var items []json.RawMessage
	if v[0] != '[' || json.Unmarshal(v, &items) != nil {
		return errors.New("not an array")
	}
	for _, it := range items {
		if err := item(it); err != nil {
			return err
		}
	}
	return nil
}
