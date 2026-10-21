package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

func TestNativeTokenCanonicalRoundtripAndDigest(t *testing.T) {
	access, refresh, err := generateNativeSecrets(rand.Reader)
	if err != nil || access.wire() == refresh.wire() {
		t.Fatalf("random generation failed: %v", err)
	}
	for _, secret := range []nativeSecret{access, refresh} {
		wire := secret.wire()
		if len(wire) != 43 {
			t.Fatal("wrong encoded length")
		}
		parsed, err := parseNativeSecret(wire)
		if err != nil || parsed.wire() != wire {
			t.Fatalf("roundtrip: %v", err)
		}
		if secret.digest() != sha256.Sum256((*secret.value)[:]) {
			t.Fatal("wrong digest")
		}
	}
}

func TestNativeTokenParserRejectsRepairableAndOversizeInputs(t *testing.T) {
	canonical := strings.Repeat("A", 43)
	invalid := []string{"", "token", canonical + "=", " " + canonical, canonical + "\n", "\n" + canonical[1:],
		"\r" + canonical[1:], "+" + canonical[1:], "/" + canonical[1:], "ä" + canonical[2:],
		canonical[:42] + "B", canonical[:42] + "\x00", strings.Repeat("a", 1<<20)}
	for i, wire := range invalid {
		secret, err := parseNativeSecret(wire)
		if err != ErrNativeUnauthorized || secret != (nativeSecret{}) {
			t.Fatalf("input %d accepted or returned a value", i)
		}
		if strings.Contains(err.Error(), wire) && wire != "" {
			t.Fatalf("input %d leaked in error", i)
		}
	}
}

type nativeBrokenReader struct{}

func (nativeBrokenReader) Read([]byte) (int, error) {
	return 0, errors.New("synthetic entropy secret detail")
}

func containsSensitiveFragment(text, wire, detail string) bool {
	if strings.Contains(text, wire) || strings.Contains(text, detail) {
		return true
	}
	return strings.Contains(text, "1 2 3 4") || strings.Contains(text, "01020304")
}

func assertDirectFormattingRedacted(t *testing.T, value any, wire, detail string) {
	t.Helper()
	var text string
	if stringer, ok := value.(fmt.Stringer); ok {
		text += stringer.String()
	}
	if stringer, ok := value.(fmt.GoStringer); ok {
		text += stringer.GoString()
	}
	if strings.Contains(text, wire) || strings.Contains(text, detail) {
		t.Fatal("direct formatting interface leaked")
	}
}

func assertValueFormattingRedacted(t *testing.T, value any, wire, detail string, formats []string) {
	t.Helper()
	for _, format := range formats {
		text := fmt.Sprintf(format, value)
		if containsSensitiveFragment(text, wire, detail) {
			t.Fatalf("formatted secret/cause leaked with format %s: %s", format, text)
		}
	}
	var out bytes.Buffer
	slog.New(slog.NewJSONHandler(&out, nil)).Info("test", "value", value)
	if strings.Contains(out.String(), wire) || strings.Contains(out.String(), detail) {
		t.Fatal("structured logging leaked")
	}
}

func TestNativeEntropyFailureAndDuplicateSecretsFailClosed(t *testing.T) {
	for _, source := range []io.Reader{nativeBrokenReader{}, bytes.NewReader(make([]byte, 31)),
		bytes.NewReader(make([]byte, 63)), bytes.NewReader(make([]byte, 64))} {
		access, refresh, err := generateNativeSecrets(source)
		if err != errNativeEntropy {
			t.Fatalf("expected errNativeEntropy, got %v", err)
		}
		if access != (nativeSecret{}) || refresh != (nativeSecret{}) {
			t.Fatal("entropy failure returned secrets")
		}
	}
}

func TestNativeSecretGrantAndStoreErrorRedaction(t *testing.T) {
	secret := newNativeSecret()
	for i := range *secret.value {
		(*secret.value)[i] = byte(i + 1)
	}
	grant := IssuedNative{access: secret, refresh: secret}
	const detail = "synthetic-private-error-detail"
	cause := fmt.Errorf("%s: %w", detail, context.Canceled)
	storeErr := nativeStoreFailure(cause)
	if !errors.Is(storeErr, context.Canceled) {
		t.Fatal("context.Canceled cause not preserved")
	}
	if !errors.Is(storeErr, cause) {
		t.Fatal("cause was not preserved")
	}
	// Loggers may call these interfaces directly rather than fmt.Formatter.
	for _, value := range []any{secret, grant, storeErr} {
		assertDirectFormattingRedacted(t, value, secret.wire(), detail)
	}
	formats := []string{"%v", "%+v", "%#v", "%s", "%q", "%d", "%b", "%o", "%O", "%x", "%X",
		"%e", "%E", "%f", "%F", "%g", "%G", "%c", "%U", "%020d", "%#x", "%.3x", "%+d"}
	for _, value := range []any{secret, &secret, grant, &grant, storeErr, &storeErr,
		struct{ Value any }{secret}, struct{ Value any }{grant}, struct{ Value any }{storeErr},
		[]any{secret, &grant, storeErr}} {
		assertValueFormattingRedacted(t, value, secret.wire(), detail, formats)
		if _, err := json.Marshal(value); !errors.Is(err, errNativeSerialization) {
			t.Fatal("implicit JSON serialization permitted")
		}
	}
}

type nativePrivateCause string

func (e nativePrivateCause) Error() string { return string(e) }

func TestNativePrivateWrapperFormattingCannotWalkSecretStorage(t *testing.T) {
	secret := newNativeSecret()
	for i := range *secret.value {
		(*secret.value)[i] = byte(i + 1)
	}
	grant := IssuedNative{access: secret, refresh: secret}
	const detail = "synthetic-private-error-detail"
	storeErr := nativeStoreFailure(nativePrivateCause(detail))
	secretOuter := struct{ secret nativeSecret }{secret}
	grantOuter := struct{ grant IssuedNative }{grant}
	errorOuter := struct{ failure error }{storeErr}
	privateErrorOuter := struct{ failure nativeStoreError }{storeErr.(nativeStoreError)}
	for valueIndex, value := range []any{secretOuter, &secretOuter, grantOuter, &grantOuter, errorOuter, &errorOuter,
		privateErrorOuter, &privateErrorOuter, reflect.ValueOf(secretOuter), reflect.ValueOf(grantOuter),
		reflect.ValueOf(errorOuter), reflect.ValueOf(secret), reflect.ValueOf(grant), reflect.ValueOf(storeErr),
		struct{ value any }{secret}, struct{ value any }{grant}, struct{ value any }{storeErr},
		struct{ value any }{&secret}, struct{ value any }{&grant}, struct{ value any }{&storeErr},
		[]any{secretOuter, grantOuter, errorOuter}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%d", "%b", "%o", "%O", "%x", "%X",
			"%e", "%E", "%f", "%F", "%g", "%G", "%c", "%U", "%020d", "%#x", "%.3x", "%+d", "%p", "%T", "%w"} {
			text := fmt.Sprintf(format, value)
			if containsSensitiveFragment(text, secret.wire(), detail) {
				t.Fatalf("private wrapper formatting exposed data: case %d, verb %s", valueIndex, format)
			}
		}
		var out bytes.Buffer
		slog.New(slog.NewJSONHandler(&out, nil)).Info("test", "value", value)
		if strings.Contains(out.String(), secret.wire()) || strings.Contains(out.String(), detail) {
			t.Fatal("private wrapper structured logging leaked")
		}
	}
	if (nativeSecret{}).wire() != "" || (nativeSecret{}).digest() != ([32]byte{}) || (nativeStoreError{}).Unwrap() != nil {
		t.Fatal("empty opaque values exposed data")
	}
}
