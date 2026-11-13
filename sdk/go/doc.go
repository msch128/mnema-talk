// Package mnema is the official Go SDK for Mnema Talk bots.
//
// It covers the bot REST API under /api/bot/v1 (with the server's rate limits
// handled for you), the gateway (heartbeats, resume and reconnects), slash
// commands, interactions, message components and modals, and the add link of
// a bot manifest. The wire contract is doc/bots.md in the Mnema Talk
// repository.
//
// A minimal bot:
//
//	c, err := mnema.New(mnema.Options{
//		BaseURL: "https://talk.example.org",
//		Token:   os.Getenv("MNEMA_TOKEN"),
//		Intents: mnema.IntentGuildMessages,
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	c.OnMessageCreate(func(ctx context.Context, m *mnema.Message) {
//		if m.Content == "!ping" {
//			_, _ = c.REST.CreateMessage(ctx, m.ChannelID, mnema.MessageCreate{Content: "pong"})
//		}
//	})
//	if err := c.Run(ctx); err != nil {
//		log.Fatal(err)
//	}
//
// # Concurrency
//
// Every gateway dispatch runs its handlers in a goroutine of its own, so a
// slow handler never stalls the heartbeats or the reading of further events.
// At most Options.HandlerConcurrency dispatches are handled at once; further
// ones wait for a free slot (in their goroutine, not in the read loop).
// Handlers of different events may therefore run concurrently and in any
// order. A panic in a handler is recovered and logged; it does not end Run.
// Run waits for running handlers before it returns, and their context is
// cancelled when Run ends.
package mnema

// Ptr returns a pointer to v, for the optional fields of edits and builders:
// mnema.MessageEdit{Content: mnema.Ptr("done")}.
func Ptr[T any](v T) *T { return &v }
