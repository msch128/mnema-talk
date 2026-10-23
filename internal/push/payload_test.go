package push

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

func notice() Notice {
	return Notice{
		ChannelID: uuid.New(), ChannelName: "allgemein", MessageID: uuid.New(),
		Sender: "Alice", Content: "Hallo zusammen", Count: 1, At: time.UnixMilli(1_700_000_000_000),
	}
}

func TestPushNamesSenderAndChannelAndCarriesTheText(t *testing.T) {
	n := notice()
	p := build(n, "de", true)
	if p.Title != "Alice (#allgemein)" || p.Body != "Hallo zusammen" {
		t.Fatalf("title %q body %q", p.Title, p.Body)
	}
	if p.Tag != n.ChannelID.String() || p.Type != "message" || p.Version != 1 || p.Timestamp != 1_700_000_000_000 {
		t.Fatalf("%+v", p)
	}
	if *p.ChannelID != n.ChannelID || *p.MessageID != n.MessageID {
		t.Fatalf("ids %+v", p)
	}
}

func TestPushWithoutPreviewNeverCarriesTheText(t *testing.T) {
	n := notice()
	n.Content = "geheimes Passwort"
	for _, c := range []struct {
		locale, want string
		mention      bool
		wantBody     string
	}{
		{"de", "", false, "Neue Nachricht"},
		{"en", "", false, "New message"},
		{"en", "", true, "Mentioned you"},
		{"de", "", true, "Hat dich erwähnt"},
		{"fr", "", false, "Neue Nachricht"},
	} {
		n.Mention = c.mention
		raw, err := build(n, c.locale, false).encode()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "Passwort") {
			t.Fatalf("%s: the text leaked into %s", c.locale, raw)
		}
		var p Payload
		if err := json.Unmarshal(raw, &p); err != nil || p.Body != c.wantBody || p.Title != "Alice (#allgemein)" {
			t.Fatalf("%s: %+v (%v)", c.locale, p, err)
		}
	}
}

func TestPushForAnAttachmentOnlyMessageSaysSo(t *testing.T) {
	n := notice()
	n.Content = "  "
	n.HasFiles = true
	if got := build(n, "en", true).Body; got != "Attachment" {
		t.Fatalf("body %q", got)
	}
	n.HasFiles = false
	if got := build(n, "en", true).Body; got != "New message" {
		t.Fatalf("body %q", got)
	}
}

func TestPushForAPollShowsTheQuestionAsAPoll(t *testing.T) {
	n := notice()
	n.Content, n.Poll = "Pizza oder Pasta?", true
	if got := build(n, "de", true).Body; got != "Umfrage: Pizza oder Pasta?" {
		t.Fatalf("body %q", got)
	}
	if got := build(n, "en", true).Body; got != "Poll: Pizza oder Pasta?" {
		t.Fatalf("body %q", got)
	}
	if got := build(n, "en", false).Body; strings.Contains(got, "Pizza") {
		t.Fatalf("a push without preview leaked the question: %q", got)
	}
}

func TestAGroupOfMessagesIsOnePush(t *testing.T) {
	n := notice()
	n.Count = 3
	withText := build(n, "en", true)
	if withText.Title != "#allgemein" || withText.Body != "3 new messages\nAlice: Hallo zusammen" {
		t.Fatalf("%q / %q", withText.Title, withText.Body)
	}
	if got := build(n, "de", false).Body; got != "3 neue Nachrichten" {
		t.Fatalf("body %q", got)
	}
}

func TestPushTextIsCleanedAndBounded(t *testing.T) {
	n := notice()
	n.Sender = "Eve‮\n\tEvil"
	n.ChannelName = strings.Repeat("k", 200)
	n.Content = "line one\r\nline\x00 two   " + strings.Repeat("ä", 300)
	p := build(n, "en", true)
	if strings.ContainsAny(p.Title+p.Body, "\n\r\t\x00‮") {
		t.Fatalf("control characters survive: %q %q", p.Title, p.Body)
	}
	if utf8.RuneCountInString(p.Body) > maxPreviewRunes || !strings.HasSuffix(p.Body, "…") || !strings.HasPrefix(p.Body, "line one line two ä") {
		t.Fatalf("body %q (%d runes)", p.Body, utf8.RuneCountInString(p.Body))
	}
	raw, _ := p.encode()
	if len(raw) > 1500 {
		t.Fatalf("payload of %d bytes", len(raw))
	}
}

func TestTestPushHasNoChannel(t *testing.T) {
	p := testPayload("en", time.UnixMilli(5))
	if p.Type != "test" || p.Body != "Push works on this device." || p.ChannelID != nil || p.Tag != "test" {
		t.Fatalf("%+v", p)
	}
	if testPayload("de", time.UnixMilli(5)).Body != "Push funktioniert auf diesem Gerät." {
		t.Fatal("German test text")
	}
}

func TestPushHidesSpoilersAndStripsMarkdown(t *testing.T) {
	n := notice()
	n.Content = "**Look** at ||the secret plan|| <:blob:11111111-2222-4333-8444-555555555555>\n> quoted"
	for _, locale := range []string{"en", "de"} {
		p := build(n, locale, true)
		if p.Body != "Look at Spoiler :blob: quoted" || strings.Contains(p.Body, "secret") {
			t.Errorf("%s: body %q", locale, p.Body)
		}
	}
}

func TestPushGroupLineHidesSpoilersToo(t *testing.T) {
	n := notice()
	n.Count = 3
	n.Content = "||hidden||"
	p := build(n, "en", true)
	if strings.Contains(p.Body, "hidden") || !strings.Contains(p.Body, "Spoiler") {
		t.Fatalf("body %q", p.Body)
	}
}
