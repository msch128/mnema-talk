package auth

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTheKeySetupNamesTheKeysWithoutRevealingThem(t *testing.T) {
	installed := (&Sessions{Secret: sessionSecretA}).KeySetup()
	if installed.Sealing != SessionDerivedKey || len(installed.Previous) != 0 {
		t.Fatalf("an instance without DATA_KEY: %+v", installed)
	}
	rotating := (&Sessions{Secret: sessionSecretB, PreviousSecret: sessionSecretA, DataKey: dataKeyTwo, DataKeyPrevious: dataKeyOne}).KeySetup()
	if rotating.Sealing != "DATA_KEY" || strings.Join(rotating.Previous, ",") != "JWT_SECRET_PREVIOUS,DATA_KEY_PREVIOUS" {
		t.Fatalf("a full rotation: %+v", rotating)
	}
	back := (&Sessions{Secret: sessionSecretA, DataKeyPrevious: dataKeyOne}).KeySetup()
	if back.Sealing != SessionDerivedKey || strings.Join(back.Previous, ",") != "DATA_KEY_PREVIOUS" {
		t.Fatalf("the way back from a data key: %+v", back)
	}

	// The fingerprint follows the sealing key and nothing else.
	if back.Fingerprint != installed.Fingerprint {
		t.Fatal("a previous key must not change the fingerprint of the sealing key")
	}
	seen := map[string]bool{}
	for _, s := range []*Sessions{{Secret: sessionSecretA}, {Secret: sessionSecretB}, {Secret: sessionSecretA, DataKey: dataKeyOne}, {Secret: sessionSecretA, DataKey: dataKeyTwo}} {
		fp := s.KeySetup().Fingerprint
		if !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(fp) || seen[fp] {
			t.Fatalf("fingerprint %q: eight hex digits, different for every key", fp)
		}
		seen[fp] = true
	}
}

func TestAPreviousDataKeyWithoutADataKeyLeadsBackToTheSessionSecret(t *testing.T) {
	id, secret := uuid.New(), []byte("twenty-byte-secret-!")
	sealed, err := (&Sessions{Secret: sessionSecretA, DataKey: dataKeyOne}).sealSecret(id, secret)
	if err != nil {
		t.Fatal(err)
	}
	back := &Sessions{Secret: sessionSecretA, DataKeyPrevious: dataKeyOne}
	got, stale, err := back.openSecret(id, sealed)
	if err != nil || !stale || !bytes.Equal(got, secret) {
		t.Fatalf("a secret under the data key that was taken out: %q stale=%v %v", got, stale, err)
	}
	again, err := back.sealSecret(id, secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, named := parseSealed(again); named {
		t.Fatal("without DATA_KEY new blobs are in the format without a key id")
	}
	// Once it is moved, the session secret alone reads it.
	if got, stale, err := (&Sessions{Secret: sessionSecretA}).openSecret(id, again); err != nil || stale || !bytes.Equal(got, secret) {
		t.Fatalf("after the move: %q stale=%v %v", got, stale, err)
	}
	// Recovery codes made under the data key verify while it is named.
	code := "abcdefghjkmn"
	under := (&Sessions{Secret: sessionSecretA, DataKey: dataKeyOne}).recoveryHash(id, code)
	found := false
	for _, h := range back.recoveryHashes(id, code) {
		found = found || bytes.Equal(h, under)
	}
	if !found {
		t.Fatal("a recovery code made under the data key must verify while it is the previous key")
	}
}
