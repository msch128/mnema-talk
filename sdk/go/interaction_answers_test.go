package mnema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnswersSendAllowedMentionsAndOnlyAMessageTakesFlags(t *testing.T) {
	d, err := Response{Content: "hi", Ephemeral: true, AllowedMentions: &AllowedMentions{Parse: []string{}}}.data()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(d)
	if got := string(raw); !strings.Contains(got, `"allowed_mentions":{}`) || !strings.Contains(got, `"flags":64`) {
		t.Fatalf("response = %s", got)
	}
	raw, _ = json.Marshal(Edit{Content: Ptr("x"), AllowedMentions: &AllowedMentions{Parse: []string{"users"}}})
	if got := string(raw); got != `{"content":"x","allowed_mentions":{"parse":["users"]}}` {
		t.Fatalf("edit = %s", got)
	}
	raw, _ = json.Marshal(Edit{Content: Ptr("x")})
	if strings.Contains(string(raw), "flags") || strings.Contains(string(raw), "allowed_mentions") {
		t.Fatalf("a plain edit sends only what is set: %s", raw)
	}
}

func TestEveryInteractionNamesItsMember(t *testing.T) {
	var i Interaction
	if err := json.Unmarshal([]byte(`{"id":"i","token":"t","type":2,"member":{"user":{"id":"u1","username":"max"}}}`), &i); err != nil || i.User.Username != "max" {
		t.Fatalf("interaction user = %+v, %v", i.User, err)
	}
	if err := json.Unmarshal([]byte(`{"id":"i","token":"t","type":2}`), &i); err == nil {
		t.Fatal("an interaction without member.user was accepted")
	}
}
