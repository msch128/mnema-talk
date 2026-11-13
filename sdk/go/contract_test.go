//go:build contract

// Contract tests against a running server: the SDK and the server must agree
// on every shape used here. They run only with MNEMA_CONTRACT_URL set:
//
//	go test -count=1 -tags=contract -run Contract ./...
//
// with MNEMA_CONTRACT_TOKEN (a bot that can view and send in the channel,
// allowed GUILDS|GUILD_MESSAGES|GUILD_MESSAGE_REACTIONS),
// MNEMA_CONTRACT_CHANNEL and MNEMA_CONTRACT_MEMBER_COOKIE (the Cookie header
// of a member who can view and send there).
package mnema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
)

const contractIntents = IntentGuilds | IntentGuildMessages | IntentGuildMessageReactions

type contractBot struct {
	env          contractEnv
	c            *Client
	run          string
	self         *Self
	ready        chan *Ready
	messages     chan *Message
	interactions chan *Interaction
	member       *member
	commandID    string
	startRun     func()
	stopRun      func() error
}

func TestContractWithARunningServer(t *testing.T) {
	env := loadContractEnv(t)
	c, err := New(Options{BaseURL: env.url, Token: env.token, Intents: contractIntents})
	if err != nil {
		t.Fatal(err)
	}
	b := &contractBot{env: env, c: c, run: fmt.Sprintf("%06x", rand.Uint32()&0xffffff),
		ready: make(chan *Ready, 4), messages: make(chan *Message, 256), interactions: make(chan *Interaction, 16), member: &member{env: env}}
	defer b.member.close()
	c.OnReady(func(_ context.Context, r *Ready) { b.ready <- r })
	c.OnMessageCreate(func(_ context.Context, m *Message) { b.messages <- m })
	c.OnInteraction(func(_ context.Context, i *Interaction) { b.interactions <- i })

	steps := []struct {
		name string
		run  func(*testing.T)
	}{
		{"Me answers the bot with its allowed intents", b.me},
		{"Run identifies and READY carries the session and the granted intents", b.connect},
		{"a posted message comes back as MESSAGE_CREATE", b.postAndReceive},
		{"a command is registered, run by the member and replied to", b.command},
		{"a button click reaches the bot, which updates the message the member sees", b.button},
		{"a modal opened on a click is submitted by the member and answered", b.modal},
		{"a burst of posts is answered 429 and the SDK waits and retries", b.burst},
		{"the command is deleted and Run ends with the context", b.finish},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	b.startRun = func() { go func() { done <- c.Run(ctx) }() }
	b.stopRun = func() error { cancel(); return <-done }
	for _, s := range steps {
		if !t.Run(s.name, s.run) {
			return
		}
	}
}

var uuidRE = regexp.MustCompile(`^[0-9a-f-]{36}$`)

func (b *contractBot) me(t *testing.T) {
	me, err := b.c.REST.Me(background(t))
	if err != nil {
		t.Fatal(err)
	}
	if !me.Bot || !uuidRE.MatchString(me.ID) || me.BotID == "" {
		t.Fatalf("me = %+v", me)
	}
	if me.AllowedIntents&contractIntents != contractIntents {
		t.Fatalf("allowed_intents %d lacks %d", me.AllowedIntents, contractIntents)
	}
	if !strings.HasPrefix(me.Handle, "bot-") {
		t.Fatalf("handle = %q, want the name members mention", me.Handle)
	}
	b.self = me
}

func (b *contractBot) connect(t *testing.T) {
	b.startRun()
	r := await(t, b.ready, "READY", func(*Ready) bool { return true })
	if r.V != 1 || r.User.ID != b.self.ID || !r.User.Bot || r.Intents != contractIntents || r.SessionID == "" || r.HeartbeatInterval <= 0 {
		t.Fatalf("ready = %+v", r)
	}
	if r.User.Handle != b.self.Handle {
		t.Fatalf("READY handle %q, @me handle %q", r.User.Handle, b.self.Handle)
	}
	if !strings.HasPrefix(r.ResumeGatewayURL, "ws://") && !strings.HasPrefix(r.ResumeGatewayURL, "wss://") {
		t.Fatalf("resume_gateway_url = %q", r.ResumeGatewayURL)
	}
}

func (b *contractBot) postAndReceive(t *testing.T) {
	ctx := background(t)
	content := "contract go " + b.run + ": hello"
	posted, err := b.c.REST.CreateMessage(ctx, b.env.channel, MessageCreate{Content: content, AllowedMentions: &AllowedMentions{Parse: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	if posted.ChannelID != b.env.channel || posted.UserID != b.self.ID || posted.Content != content {
		t.Fatalf("posted = %+v", posted)
	}
	got := await(t, b.messages, "MESSAGE_CREATE", func(m *Message) bool { return m.Content == content })
	if got.ID != posted.ID || !got.IsBot {
		t.Fatalf("event = %+v", got)
	}
	again, err := b.c.REST.Message(ctx, b.env.channel, posted.ID)
	if err != nil || again.Content != content {
		t.Fatalf("Message = %+v, %v", again, err)
	}
}

func (b *contractBot) command(t *testing.T) {
	ctx := background(t)
	cmd, err := b.c.REST.CreateCommand(ctx, CommandInput{Name: "contract-echo-go", Description: "Echoes the text (Go contract test)",
		Options: []CommandOption{{Name: "text", Description: "What to echo", Type: OptionString, Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "contract-echo-go" || len(cmd.Options) != 1 || cmd.Options[0].Type != OptionString || !cmd.Options[0].Required {
		t.Fatalf("command = %+v", cmd)
	}
	b.commandID = cmd.ID
	list, err := b.c.REST.Commands(ctx)
	if err != nil || !hasCommand(list, cmd.ID) {
		t.Fatalf("Commands = %+v, %v", list, err)
	}
	var invoked struct {
		InteractionID string `json:"interaction_id"`
	}
	b.member.call(t, http.MethodPost, "/api/channels/"+b.env.channel+"/interactions", map[string]any{"command_id": cmd.ID, "options": map[string]any{"text": b.run}}, http.StatusAccepted, &invoked)
	i := await(t, b.interactions, "the command interaction", func(i *Interaction) bool { return i.IsCommand() && i.CommandName() == "contract-echo-go" })
	if i.ID != invoked.InteractionID || i.ChannelID != b.env.channel || i.Data.ID != cmd.ID || i.Option("text").String() != b.run || i.User.ID == "" {
		t.Fatalf("interaction = %+v", i)
	}
	if err := i.Reply(ctx, Response{Content: "echo: " + b.run}); err != nil {
		t.Fatal(err)
	}
	m := await(t, b.messages, "the reply", func(m *Message) bool { return m.Content == "echo: "+b.run })
	if m.UserID != b.self.ID {
		t.Fatalf("reply = %+v", m)
	}
	answersFollowTheirRoutes(t, i)
}

// answersFollowTheirRoutes sends what each answer route takes:
// allowed_mentions on follow-ups and @original, flags on follow-ups only.
func answersFollowTheirRoutes(t *testing.T, i *Interaction) {
	ctx := background(t)
	name := "@" + i.User.Username
	quiet, err := i.FollowUp(ctx, Response{Content: "quiet " + name, AllowedMentions: &AllowedMentions{Parse: []string{}}})
	if err != nil || quiet == nil || quiet.MentionsUser(i.User.ID) {
		t.Fatalf("quiet follow-up = %+v, %v", quiet, err)
	}
	loud, err := i.FollowUp(ctx, Response{Content: "loud " + name})
	if err != nil || loud == nil || !loud.MentionsUser(i.User.ID) {
		t.Fatalf("loud follow-up = %+v, %v", loud, err)
	}
	if note, err := i.FollowUp(ctx, Response{Content: "psst", Ephemeral: true}); err != nil || note != nil {
		t.Fatalf("ephemeral follow-up = %+v, %v", note, err)
	}
	edited, err := i.EditReply(ctx, Edit{Content: Ptr("edited " + name), AllowedMentions: &AllowedMentions{Parse: []string{}}})
	if err != nil || edited == nil || edited.MentionsUser(i.User.ID) {
		t.Fatalf("edited = %+v, %v", edited, err)
	}
	_, err = call[Message](ctx, i.rest, http.MethodPatch, i.path("/messages/@original"), map[string]any{"content": "x", "flags": FlagEphemeral})
	var re *RESTError
	if !errors.As(err, &re) || re.Status != http.StatusBadRequest || !strings.Contains(re.Message, "flags cannot be set on @original") {
		t.Fatalf("flags on @original: %v", err)
	}
}

func hasCommand(list []Command, id string) bool {
	for _, c := range list {
		if c.ID == id {
			return true
		}
	}
	return false
}

func (b *contractBot) button(t *testing.T) {
	ctx := background(t)
	id := "click:" + b.run
	posted, err := b.c.REST.CreateMessage(ctx, b.env.channel, MessageCreate{Content: "contract go " + b.run + ": click me", Components: []Component{ActionRow(PrimaryButton(id, "Click"))}})
	if err != nil {
		t.Fatal(err)
	}
	if len(posted.Components) != 1 || posted.Components[0].Components[0].CustomID != id {
		t.Fatalf("posted components = %+v", posted.Components)
	}
	b.member.call(t, http.MethodPost, "/api/channels/"+b.env.channel+"/messages/"+posted.ID+"/components", map[string]any{"custom_id": id}, http.StatusAccepted, nil)
	i := await(t, b.interactions, "the click", func(i *Interaction) bool { return i.IsButton() && i.CustomID() == id })
	if i.Message == nil || i.Message.ID != posted.ID || i.Data.ComponentType != ComponentButton {
		t.Fatalf("click = %+v", i)
	}
	content := "contract go " + b.run + ": clicked"
	if err := i.Update(ctx, Edit{Content: &content, Components: &[]Component{}}); err != nil {
		t.Fatal(err)
	}
	seen := b.member.message(t, posted.ID)
	if seen["content"] != content {
		t.Fatalf("the member sees %v", seen["content"])
	}
	if comps, ok := seen["components"].([]any); ok && len(comps) > 0 {
		t.Fatalf("the components were not removed: %v", comps)
	}
}

func (b *contractBot) modal(t *testing.T) {
	ctx := background(t)
	b.member.listen(t)
	id := "form:" + b.run
	posted, err := b.c.REST.CreateMessage(ctx, b.env.channel, MessageCreate{Content: "contract go " + b.run + ": feedback", Components: []Component{ActionRow(PrimaryButton(id, "Feedback"))}})
	if err != nil {
		t.Fatal(err)
	}
	b.member.call(t, http.MethodPost, "/api/channels/"+b.env.channel+"/messages/"+posted.ID+"/components", map[string]any{"custom_id": id}, http.StatusAccepted, nil)
	click := await(t, b.interactions, "the click", func(i *Interaction) bool { return i.IsButton() && i.CustomID() == id })
	title := "Feedback " + b.run
	if err := click.ShowModal(ctx, NewModal("fb:"+b.run, title, TextInput{CustomID: "subject", Label: "Subject"})); err != nil {
		t.Fatal(err)
	}
	var opened struct {
		ID         string      `json:"id"`
		Title      string      `json:"title"`
		ChannelID  string      `json:"channel_id"`
		Components []Component `json:"components"`
	}
	raw := b.member.event(t, "interaction_modal", func(p json.RawMessage) bool { return strings.Contains(string(p), title) })
	if err := json.Unmarshal(raw, &opened); err != nil || opened.ChannelID != b.env.channel || opened.Components[0].Components[0].CustomID != "subject" {
		t.Fatalf("interaction_modal = %s (%v)", raw, err)
	}
	b.member.call(t, http.MethodPost, "/api/channels/"+b.env.channel+"/modals/"+opened.ID, map[string]any{"values": map[string]string{"subject": "Login"}}, http.StatusAccepted, nil)
	s := await(t, b.interactions, "the modal submit", func(i *Interaction) bool { return i.IsModalSubmit() && i.CustomID() == "fb:"+b.run })
	if s.Field("subject") != "Login" || s.Message == nil || s.Message.ID != posted.ID {
		t.Fatalf("submit = %+v", s)
	}
	if err := s.Reply(ctx, Response{Content: "thanks " + b.run, Ephemeral: true}); err != nil {
		t.Fatal(err)
	}
}

func (b *contractBot) burst(t *testing.T) {
	ctx := background(t)
	counter := &statusCounter{counts: map[int]int{}}
	raw, err := NewREST(Options{BaseURL: b.env.url, Token: b.env.token, MaxRetries: 5, HTTPClient: &http.Client{Transport: counter}})
	if err != nil {
		t.Fatal(err)
	}
	// Without the local limit the server's per-channel limit answers 429.
	raw.limits.post = window{limit: 1000, every: postWindow}
	var wg sync.WaitGroup
	posted := make([]*Message, 7)
	errs := make([]error, 7)
	for n := range 7 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			posted[n], errs[n] = raw.CreateMessage(ctx, b.env.channel, MessageCreate{Content: fmt.Sprintf("contract go %s: burst %d", b.run, n)})
		}()
	}
	wg.Wait()
	for n, err := range errs {
		if err != nil {
			t.Fatalf("post %d: %v", n, err)
		}
	}
	if counter.count(http.StatusTooManyRequests) == 0 {
		t.Fatal("the server did not answer 429 to 7 posts in one channel")
	}
	for _, m := range posted {
		if err := b.c.REST.DeleteMessage(ctx, b.env.channel, m.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func (b *contractBot) finish(t *testing.T) {
	ctx := background(t)
	if err := b.c.REST.DeleteCommand(ctx, b.commandID); err != nil {
		t.Fatal(err)
	}
	list, err := b.c.REST.Commands(ctx)
	if err != nil || hasCommand(list, b.commandID) {
		t.Fatalf("Commands after delete = %+v, %v", list, err)
	}
	if err := b.stopRun(); err != nil {
		t.Fatalf("Run after cancel = %v", err)
	}
}
