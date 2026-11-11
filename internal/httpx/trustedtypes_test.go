package httpx

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// directive returns the value of one directive of a policy.
func directive(policy, name string) string {
	for part := range strings.SplitSeq(policy, ";") {
		fields := strings.Fields(part)
		if len(fields) > 0 && fields[0] == name {
			return strings.Join(fields[1:], " ")
		}
	}
	return ""
}

func TestSPAPolicyRequiresTrustedTypes(t *testing.T) {
	if got := directive(SPAContentSecurityPolicy, "require-trusted-types-for"); got != "'script'" {
		t.Errorf("require-trusted-types-for = %q, want 'script'", got)
	}
	// Exactly the policies the web app creates: Vue's, the Markdown renderer,
	// the worker script addresses and the patched emoji picker.
	names := directive(SPAContentSecurityPolicy, "trusted-types")
	if names != "vue mnema-markdown mnema-script-url mnema-emoji-picker" {
		t.Errorf("trusted-types = %q", names)
	}
	// A catch-all policy or repeated names would let any script make trusted values.
	for _, loose := range []string{"default", "'allow-duplicates'", "*"} {
		if slices.Contains(strings.Fields(names), loose) {
			t.Errorf("trusted-types must not list %s", loose)
		}
	}
}

func TestTrustedTypesAreEnforcedOnTheAppNotReported(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(nil, false)(okHandler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := directive(rec.Header().Get("Content-Security-Policy"), "require-trusted-types-for"); got != "'script'" {
		t.Errorf("enforced policy: require-trusted-types-for = %q, want 'script'", got)
	}
	if got := rec.Header().Get("Content-Security-Policy-Report-Only"); got != "" {
		t.Errorf("Content-Security-Policy-Report-Only = %q, want none", got)
	}
}

func TestAPIDocsPolicyIsTheAppPolicyWithoutTrustedTypes(t *testing.T) {
	want, rest, found := strings.Cut(SPAContentSecurityPolicy, "; require-trusted-types-for ")
	if !found || strings.Contains(rest, "-src") {
		t.Fatalf("the Trusted Types directives are expected last in %q", SPAContentSecurityPolicy)
	}
	if APIDocsContentSecurityPolicy != want {
		t.Errorf("API docs CSP = %q, want %q", APIDocsContentSecurityPolicy, want)
	}
}
