package chat

import (
	"fmt"
	"strings"
	"testing"
)

func TestNativeCiphertextPrivateWrapperFormatting(t *testing.T) {
	data := &nativeCiphertextData{groupID: []byte("fixture-group-marker"), ciphertext: []byte("fixture-ciphertext-marker")}
	record := NativeCiphertextRecord{value: &data}
	for _, v := range []any{record, struct{ record NativeCiphertextRecord }{record}, struct {
		nested struct{ record NativeCiphertextRecord }
	}{struct{ record NativeCiphertextRecord }{record}}} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%d", "%x", "%X", "%f"} {
			formatted := fmt.Sprintf(verb, v)
			if strings.Contains(formatted, "fixture-group-marker") || strings.Contains(formatted, "fixture-ciphertext-marker") || strings.Contains(formatted, "66697874757265") {
				t.Fatalf("opaque data traversed for %s", verb)
			}
		}
	}
	wire := record.Wire()
	wire.GroupID[0] = 0
	wire.Ciphertext[0] = 0
	if record.Wire().GroupID[0] != 'f' || record.Wire().Ciphertext[0] != 'f' {
		t.Fatal("wire aliases record")
	}
	if (NativeCiphertextRecord{}).Wire().Ciphertext != nil {
		t.Fatal("empty record fabricated bytes")
	}
}
