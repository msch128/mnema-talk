package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSignedTokenCarriesTheSessionClaimsAndVerifies(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tok := sign("s3cret", "11111111-2222-3333-4444-555555555555", 7, now, time.Hour)
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts", len(parts))
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)); parts[2] != want {
		t.Fatal("signature does not verify with the secret")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var c claims
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if c.UID != c.Sub || c.TV != 7 || c.Iss != "mnema-talk" || c.Exp-c.Iat != 3600 || c.Nbf != c.Iat {
		t.Fatalf("claims = %+v", c)
	}
	head, _ := base64.RawURLEncoding.DecodeString(parts[0])
	if string(head) != `{"alg":"HS256","typ":"JWT"}` {
		t.Fatalf("header = %s", head)
	}
}

func TestMintReadsPsqlLinesAndRejectsBadOnes(t *testing.T) {
	got, err := mint(strings.NewReader("aaa,1\n\n bbb|2 \n"), "k", time.Now(), time.Hour)
	if err != nil || len(got) != 2 || got[0].ID != "aaa" || got[1].ID != "bbb" {
		t.Fatalf("got %+v, err %v", got, err)
	}
	for _, bad := range []string{"no-comma", ",3", "id,x"} {
		if _, err := mint(strings.NewReader(bad), "k", time.Now(), time.Hour); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
}
