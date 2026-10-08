package ws

import (
	"strings"
	"testing"
)

func TestNativeRenewStrictEnvelope(t *testing.T) {
	token := strings.Repeat("A", 43)
	good := `{"type":"native_access_renew","payload":{"access_token":"` + token + `"}}`
	for _, raw := range []string{good, `{"payload":{"access_token":"` + token + `"},"type":"native_access_renew"}`} {
		if got, ok := parseNativeRenewFrame([]byte(raw)); !ok || got != token {
			t.Fatal("canonical native renewal rejected")
		}
	}
	for _, raw := range []string{"", "null", "[]", good + " {}", strings.Repeat("x", 1025),
		`{"type":"native_access_renew","type":"native_access_renew","payload":{"access_token":"` + token + `"}}`,
		`{"type":"native_access_renew","payload":{"access_token":"` + token + `","access_token":"` + token + `"}}`,
		`{"type":"native_access_renew","payload":{"access_token":"` + token + `"},"extra":true}`,
		`{"type":"native_access_renew","payload":null}`, `{"type":"native_access_renew","payload":{}}`,
		`{"type":"native_access_renew","payload":{"access_token":null}}`,
		`{"type":"native_access_renew","payload":{"access_token":"short"}}`,
		`{"type":"native_access_renew"}`, `{"payload":{"access_token":"` + token + `"}}`,
		`{"type":"other","payload":{"access_token":"` + token + `"}}`,
		`{"type":"native_access_renew","payload":{"access_token":"` + token + `"},"payload":{"access_token":"` + token + `"}}`,
		`{"type":"native_access_renew","payload":{"access_token":"` + token + `"}} private`,
		`{"type":"native_access_renew","payload":{"access_token":"` + token + `"]}`,
		`{"type":"native_access_renew","payload":{"access_token":"` + token + `"`,
		`{"type":`, `{"payload":{"access_token":`, `{"payload":{"access_token":"` + token + `"},`, `{"payload":`, `{,`} {
		if _, ok := parseNativeRenewFrame([]byte(raw)); ok {
			t.Fatal("ambiguous native renewal accepted")
		}
	}
}
