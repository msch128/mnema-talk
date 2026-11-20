# Review of bot-sdk-go PR #9 and bot-sdk-js PR #18: the bot token stays with the configured server

Two rounds each. The should-fix findings of the first round were fixed in the
same pull requests and checked in the second.

What the pull requests change (from the 2026-11-19 audit of the SDKs):

- Go: READY's `resume_gateway_url` was dialled as given, with the bot token
  in the header (the JS SDK already kept the configured host); the REST client
  followed redirects with Go's default policy, which keeps `Authorization` on
  a redirect to the same host on another scheme or port; error strings of
  failed requests carried interaction tokens.
- JS: interaction tokens in `RestError.path`, the `onRateLimit` info and the
  debug and rate-limit logs; JSON and error answers read without a size
  limit.

## bot-sdk-go #9

### Round 1

**Verdict: MERGE AFTER FIXES.** Read in full: `rest.go`, `gateway.go`,
`ratelimit.go`, `rest_download.go`, `errors.go`, and the token paths of
`gateway_conn.go`, `interaction_respond.go` and `client.go`. The redaction
covers the four interaction routes of the Talk server
(`internal/bot/interactions.go`). Run: gofmt, go vet (both tag sets),
`go test -race`, gocognit and gocyclo over 15, staticcheck 0.8.2: clean.

- Should-fix `gateway.go:197-214`: the resume path of another host was put on
  the dialled host, so a gateway behind a proxy path (`ws://talk:8080` with a
  public address `https://example.org/talk`) dialled an address that does not
  exist on every reconnect. The JS SDK uses the base address unchanged when
  the host or port differ.
- Nits: the `..` check ran on the raw string (`%2e%2e` passed); the first
  dial's `GET /gateway` address is not checked (the server gives both); the
  `webhooks` match serves no SDK route today; downloads formatted their error
  by hand; the docs did not mention the fallback; `Interaction.Token` is an
  exported field (pre-existing).
- Confirmed: redirects are a `*RESTError`, and the test uses the case where
  Go's policy would forward the header; `requestError` rewrites the wrapped
  `*url.Error`; the gorilla dialer follows no redirects.

### Round 2 (bc93576)

**Verdict: MERGE.** A resume address is used only when it is relative or
ws/wss with the same hostname (case-insensitive) and port (default ports
filled in); anything else dials the configured address unchanged, with a test
for the proxy-path case. `..` is checked on the decoded segments (`%2e%2e`
refused, `..` in a query kept). Downloads use `requestError`. Same checks run,
clean.

## bot-sdk-js #18

### Round 1

**Verdict: MERGE.** Read in full: `src/rest/core.ts`, `body.ts`, `error.ts`,
`limits.ts` and the tests, plus the token paths of the client and gateway.
Tokens are encoded into their own path segment, so a `/` or `?` in a token
cannot move it. The 16 MiB cap refuses only above the cap (exactly 16 MiB is
read), cancels the stream, and catches a compressed Content-Length while
streaming. Run: `npm run check` exit 0, 97 tests.

- Should-fix `src/rest/core.ts:108,201`: a rejected `fetch` was passed on as
  is; a custom `RestOptions.fetch` such as node-fetch puts the full URL, token
  included, into its message.
- Should-fix (parity): `send()` followed redirects, downloads did not.
- Nits: an error answer over the cap lost its HTTP status; the docs said "like
  the Go SDK"; one size test did not prove the Content-Length path; the
  changed `path` values deserved a behaviour-change note; `Interaction.token`
  is a public property (pre-existing).

### Round 2 (e48c025)

**Verdict: MERGE.** A rejected fetch becomes `RequestFailedError` with the
redacted path, the token removed raw and decoded from the message, and no
`cause` on token routes; `send()` uses `redirect: 'manual'` and a 3xx is a
`RestError` (a two-server test shows the target gets no request);
`ResponseTooLargeError` carries the status; the Content-Length test asserts no
bytes were pulled. Run: `npm run check` exit 0, 100 tests.

Remaining nits: with Node's own fetch the message reads "fetch failed" and the
reason is on the dropped `cause`; callers that checked `TypeError` after a
network failure now get `RequestFailedError` (not marked as a behaviour
change); an `AbortError` from a custom fetch is passed on unchanged.

Not checked in either round: the contract tests against a live server, proxy
setups, custom fetch implementations other than the test stub.
