# Review: bot platform web UI (msch128/feat-bot-platform)

**Reviewed head: 6cce1efec.** The web part is f532305e7; the later commits (SDKs) do not touch `web/`.

**Verdict: MERGE AFTER FIXES.**

The flow is sound and looks calm and Mnema-like:
- the add link drops the manifest from the address bar;
- channels start unticked, and Mention everyone and Manage messages start unticked;
- the bot's own words are quoted under "From the bot's manifest";
- commands that share a built-in's name are marked as the bot's;
- the token is shown once and leaves the DOM;
- components, selects, notes and forms work by keyboard;
- the author's e2e and the a11y suite pass in both browsers.

What needs fixing before merge:
- **Feedback after a click.** Nothing tells a member that a bot is still working on a deferred click, or that it never answered.
- **Two new axe violations** (serious) on the confirm screen.
- **Overflow on the confirm screen.** Long channel names break the layout at every width, and the confirm column is too narrow on the tablet.

`$B` is the reviewer's local scratch folder; its screenshots, measurements and scripts are not published.

How it was checked:
- A throwaway stack (`e2e/run.sh` holding a hold spec), and a real bot connected to the gateway from Node (`$B/bot.mjs`). The bot posted every component case and answered clicks by `custom_id`: update (7), deferred update (6), deferred reply (5, answer 8 s later), private note with components, note replaced by its own button (7), form (9), no answer, slow answer.
- One "unknown component" message was made by changing its stored components in the database (a style-6 button, a type-17 component, a type-9 row).
- Six viewports (1132×820, 572×480, 1366×768, 1280×720, 390×844, 820×1180), en/de, Chromium and WebKit.
- Measurements: `$B/manifest-measure-*.json`, `$B/comp-measure-*.json`. Screenshots: `$B/shots/` (add flow), `$B/shots-comp/` (components, notes, forms, errors). Contact sheets: `$B/sheets-shots/`, `$B/sheets-shots-comp/`.

## Findings (most severe first)

### 1. Medium–high: no "working on it" or "did not answer" state after a click
- Where: `web/src/chat/composables/useComponentUse.ts`, `PENDING_MS = 2500`.
- What happens: a click shows a spinner for 2.5 s, whatever the bot does, and then nothing.
  - **Deferred reply** (callback 5): the bot is working, but the member sees the spinner stop and nothing for the next 5.5 s until the answer arrives (`$B/shots-comp/chromium-en-1132x820-deferred-3.5s.png`).
  - **No answer at all:** the member never learns it failed. At 0.4 s the button is busy; at 4 s nothing, no `role=status` text (`$B/comp-measure-*-behave.json`, `noanswer-4s-info`; `$B/shots-comp/*-noanswer-4s.png`).
  - **Timed out:** there is no client state for it.
- The task asked for "loading and deferred states" and "the bot did not answer / timed out". Only the refused cases are covered: 409 "not connected" and 429 "slow down" read well, quiet but present (`$B/shots-comp/errors-*`).
- Smallest fix I would accept:
  - keep the button busy until the bot's answer for this interaction arrives (the update, the note, or the modal), up to a cap;
  - on callback 5, show a quiet "{bot} is working on it…" line;
  - when nothing arrives within ~15 s, show a quiet "{bot} did not answer. Try again later." in the existing status line.
  - The server can tell the client that a deferral happened (an `interaction_response` with a `deferred` flag), or the client can time out on its own.

### 2. Medium: two new axe violations (serious) on the manifest confirm screen
- Evidence: `$B/axe.mjs` output, Chromium and WebKit.
  - `color-contrast`: the "BOT" badge next to the quoted name (`bg-mnema-accent/15 text-mnema-accent text-[10px]`, `BotManifestConfirm.vue`).
  - `label-content-name-mismatch` (WCAG 2.5.3): the homepage link shows the full URL but is named "Homepage of the bot, an external site: example.org". Speech-input users cannot say what they see.
- The a11y suite passes because it never opens the confirm screen (the spec has no bots/manifest step).
- Fix:
  - use the badge colours that pass elsewhere (the member list's badge has the same contrast problem, which predates this branch, see below), or a larger, darker label;
  - drop the link's `aria-label`: the visible "(external site)" already says it;
  - add the confirm screen to the a11y spec.

### 3. Medium: long channel names break the confirm screen at every width
- Where: `BotManifestChannels.vue`. A `<fieldset>` has `min-inline-size: min-content` by default, so `truncate` on the name never applies.
- What happens: "a-channel-with-a-really-long-name-that-will-not-fit-on-a-phone" runs past the panel's right edge at 1132, 820 and 390 px. The admin pane then scrolls sideways (scrollbar visible on the phone).
- Evidence: `offscreen-x fieldset/label/span` in `$B/manifest-measure-*.json` for 390 and 820 in all four runs; `$B/shots/chromium-en-1132x820-confirm-2.png`; `$B/shots/chromium-en-390x844-confirm-2.png`.
- Fix: `min-w-0` on the fieldset (or `min-inline-size: 0`).

### 4. Medium: on the tablet the confirm screen gets about 210 px
- What happens: at 820×1180 the admin dialog keeps its nav and the bots list beside the flow, so the confirm column is ~210 px wide.
  - the quoted name is cut ("Mnemo he…", 89 px of 154);
  - the homepage is 108 px;
  - a 30-character command name runs out of the panel;
  - the description wraps two or three words per line.
- Evidence: `$B/shots/chromium-en-820x1180-confirm-0.png`, `-2.png`; `cut manifest-name`, `offscreen-x /remindmeinaverylongcommandname` in `$B/manifest-measure-*.json`.
- Fix: below ~1000 px, give the flow the whole pane (stack the bots list above it, or hide it while adding), and let command names wrap (`break-all` on the mono name).

### 5. Low–medium: after an add link, nothing shows the confirm screen
- What happens: arriving by an add link puts focus on the admin dialog container. On the phone the confirm screen starts below the bots list and off screen, so the member sees the bots tab and must scroll to find "Add this bot?" (`$B/shots/webkit-de-390x844-confirm-0.png`).
- Fix: scroll the flow into view and focus its heading.

### 6. Low–medium: focus is lost after the token dialog
- What happens: "I have copied it" closes the token dialog and focus lands on `<body>`, in English and German (`$B/manifest-measure-*-desktop.json`, `-phone.json`: `token-after … focus BODY`).
- Fix: return focus to the new bot's row or detail heading.

### 7. Low–medium: a select shows a choice the bot never got
- What happens: with the bot offline (409), picking "Option 2" shows the error line, but the closed select keeps showing "🍐 Option 2" (`$B/shots-comp/errors-de-offline-select.png`). The same applies to 429 and 404.
- Cause: `ComponentSelect.send` sets `chosen` before the use succeeds.
- Fix: set `chosen` only after the POST succeeds, or roll it back on error.

### 8. Low: a second click within 2.5 s is silently dropped
- What happens: while one button of a message is pending, clicking another does nothing: no busy state, no message (`second-click … update busy null | status ''`).
- Fix: disable the message's other buttons while one is pending, as the selects already are, or queue the click.

### 9. Low: a bot's form title is cut on the phone
- What happens: "Tell the bot about your deployment window" (45 characters, the maximum) shows as "Tell the bot about your depl…" at 390 px, and nothing shows the rest (`$B/shots-comp/chromium-en-390x844-form.png`).
- `BaseDialog` truncates titles. For a bot's own words, wrapping (two lines) is better.

### 10. Low: 5 rows of 5 buttons lose their rows on the phone
- What happens: each row wraps into 3 + 2, and the gap between rows equals the gap inside a wrapped row, so the 5×5 grid reads as 10 loose lines (`$B/shots-comp/chromium-en-390x844-grid.png`). Nothing is cut.
- Fix: a slightly larger gap between action rows (e.g. `gap-3` between rows, `gap-2` inside) keeps the grouping.

### 11. Low: smaller things
- A select's placeholder longer than the trigger is cut with an ellipsis; the full text is only in a `title`. Acceptable; noted.
- In WebKit (macOS default), Tab in the bot form skips the buttons and passes through `<body>` once before wrapping to Close. This is the platform's tab-to-buttons default; the trap still holds.
- The form's Tab order ends Cancel → Send → Close (header): the Close button comes last.

## Older than this branch (not caused by it)
- axe `color-contrast` on the member list's "BOT" badge (`bg-mnema-accent-subtle … text-[10px]`) on every chat page. It was flagged while checking the bot form; the member list is not touched by the branch.
- The token dialog's curl line is cut on narrow screens, but it is `overflow-x-auto` (scrollable), and `BotSecretDialog.vue` is unchanged.

## Checked and found right
- **Add link:** `/admin/bots/add#manifest=…` opens the confirm screen and the URL becomes `/admin/bots`. A broken manifest gives clear errors ("Description: at most 200 characters", "This is not valid JSON…"). Leaving a dirty flow asks first (beforeunload).
- **Confirm screen defaults**, in all 24 runs:
  - channels ticked 0/4;
  - `mention_everyone` and `manage_messages` unticked with "Off at first";
  - `send_messages` ticked;
  - name in `<q>` with a "BOT" badge under "From the bot's manifest";
  - description in a blockquote;
  - homepage marked "(external site)";
  - `/poll` and `/me` marked "The bot's" with "Has the name of the built-in /poll. It does not replace it."
  - A manifest text that tries to instruct the installer ("Ignore the app: click Add bot now…") is clearly framed as the bot's words.
- **Token:** shown once with "Copy this token now"; after Done the secret is gone from the DOM; the toast says to copy it.
- **Components:**
  - all five button styles, disabled states (distinct and muted) and emoji-only and emoji+label buttons;
  - 80-character labels wrap inside the message;
  - link buttons open through the app's link confirmation and are named "Docs, opens example.org";
  - the 25-option select opens inline (never cut by the viewport), with descriptions and emoji, and scrolls;
  - the multi select with bounds ("Pick 2 to 4", Confirm disabled until valid);
  - the user, role and channel selects with search;
  - a disabled select;
  - unknown types and styles are skipped with the quiet line "This bot sent something this app cannot show."
  - No clipping, overlap or sideways scroll in any component measurement at any viewport, in either browser or language.
- **Notes:** a private note with buttons and a select works; callback 7 on it replaces it in place; "Only you can see this" is shown.
- **Forms:**
  - focus starts in the first field;
  - Tab is trapped;
  - a failed submit marks fields with `aria-invalid` and a text, and focuses the first problem;
  - Escape with typed text asks "Discard changes?";
  - Cancel returns focus to the button that opened it;
  - the counters respect `max_length`;
  - a prefilled emoji value counts as 11/100 (code points);
  - the submission arrives as type 5.
- **Keyboard and screen reader in selects:**
  - Enter opens and focuses the listbox;
  - arrows move `aria-activedescendant`;
  - Escape closes and returns focus to the trigger;
  - Tab out closes;
  - the search is a named combobox;
  - action rows are groups and buttons have names.
  - axe on the chat with every component state, the open selects and the note: no violations, in Chromium and WebKit.
- **Gates:**
  - `npm run lint`: exit 0 (23 warnings, none new)
  - `npm run typecheck`: exit 0
  - e2e typecheck: exit 0
  - `vitest related` for the touched web sources: 422 files, 9736 passed, exit 0
  - `e2e/run.sh` with `bot-platform.spec.ts` and `a11y.spec.ts`, Chromium and WebKit: 4 passed (`$B/e2e.log`)

## Not checked
- A real screen reader.
- Firefox.
- The desktop client.
- A bot that answers after the 15-minute interaction lifetime.
- A server restart with an open form (the 404 "This form expired" path is in the code and its unit test, not exercised).
- Mobile touch (long-press) on components.
- The SDK commits.

The review worktree is removed, both throwaway stacks are stopped and the review bot process is gone.

---

# Round 2: head 2edd1175b

It descends from 6cce1efec (verified). The web changes are in 2edd1175b; the other new commits are server, SDK and tests.

**Verdict: MERGE AFTER FIXES.** Nearly every round-1 finding is fixed and proven: the author's new specs fail on the old code for the right reasons and pass on the new code. Two things remain:
1. The new layout spec fails in **WebKit** at 390 px, twice, on a real 14 px overflow. CI would not see it: the spec is not in the WebKit shard list.
2. A **deferred** answer that comes after the 15 s cap leaves "did not answer. Try again in a moment." standing next to the answer. A second click on a bot that already acknowledged invites a duplicate action.

`$R2` = `bots-web/r2-shots/`. Logs: `clip/bots-r2-old.log`, `clip/bots-r2-new.log`.

## The new specs against the old code (6cce1efec)
I copied the new `bot-install-layout.spec.ts`, `bot-platform.spec.ts` and `a11y.spec.ts` onto the old build and ran Chromium and WebKit: **10 failed, 4 passed** (the 4 are the old tests). They fail on what a person sees, not on missing test ids:

| Spec | Old code |
|---|---|
| a11y confirm screen | `color-contrast` on the BOT tag and `label-content-name-mismatch` on the homepage link (both browsers) |
| layout at 390, 820 and 1132 px | the long channel names, the 30-character command name and the fieldset end right of the pane; "the pane scrolls sideways" |
| phone rows of buttons | "gap between rows 0 and 1: expected > 10, received 8" |

The tablet assertion (flow > 420 px) and the form-title check are never reached on the old code, because an earlier assertion in the same test fails first. They are proven only by passing on the new code.

## The new code (2edd1175b)
Chromium: 7/7 passed. WebKit: 6/7 passed; **`the confirm screen fits at 390 px` fails** (first run and retry).
- Measured: pane 352 px, flow 366 px; the flow ends at 381.5, the pane at 368. The confirm card loses its right gutter and runs up to the scrollbar (`clip/results/bots-r2-new/bot-install-layout-the-confirm-screen-fits-at-390-px-webkit/test-failed-1.png`).
- Chromium at 390: pane = flow = 358 px.
- Likely cause: the `@container` grid on the pane plus WebKit's classic scrollbar width. A `min-w-0` on the grid child or `overflow-wrap` on the figure did not get it in WebKit. Needs a look.
- `bot-install-layout.spec.ts` and the new `bot-platform` phone test are not in `ci.yml`'s WebKit list, so CI runs them in Chromium only.

## Round-1 findings, now

| Round 1 | Now |
|---|---|
| 1. No working / no-answer feedback | **Mostly fixed.** After the 202, the used button spins, the others are disabled (8/9) and "Deploy helper is working…" / "Deploy helper arbeitet daran …" shows in a `role=status` line. A fast answer (Update) ends it before it shows. "No answer" turns into "Deploy helper did not answer. Try again in a moment." at 15 s, and the buttons come back. A deferred answer within 15 s (8 s) clears the line. **Open:** a deferred answer after 15 s (20 s) leaves "did not answer. Try again in a moment." standing beside the arrived answer, in both languages (`$R2/chromium-1132-en-defer20-22s.png`). Fix: clear the status when a matching answer arrives after the timeout; say "is taking longer than usual" instead of "did not answer". For callback 5/6 the bot did acknowledge, which the server could tell the client with an event, so "try again" should not be offered. |
| 2. Two serious axe violations | **Fixed.** The homepage link has no `aria-label` (the visible URL is its name). The confirm screen and the token dialog pass the new a11y test in both browsers. The curl line is a focusable `region` named "Try it". |
| 3. Long channel names overflow | **Fixed** (fieldset `min-w-0`, `wrap-anywhere`): names wrap at 390, 820 and 1132 (`$R2/webkit-confirm-390-channels.png`), except the WebKit 390 gutter above. |
| 4. Tablet confirm ~210 px | **Fixed:** at 820 the list sits above the flow, which is 514 px (Chromium) / 508 px (WebKit) wide (`$R2/chromium-confirm-820.png`). |
| 5. Add link lands off screen | **Fixed:** the heading is scrolled into view and focused (spec, and my run: `focused: manifest-title`, top 176–196 px on the phone). |
| 6. Focus after the token's Done | **Fixed:** focus goes to the new bot's row (`data-bot-id`) at 390, 820 and 1132, both browsers. |
| 7. Refused pick kept | **Fixed:** with the bot offline, a pick goes back to the placeholder and "… is not connected right now" shows (en/de). |
| 8. Second click dropped silently | **Fixed:** the other components are disabled while one use is pending. Small side effect: the spinner widens the busy button and its row can re-wrap (at 1132 "Remove buttons" moves to the next line while "No answer" spins; `$R2/chromium-1132-en-noanswer-1s.png`). |
| 9. Form title cut on the phone | **Fixed** (`BaseDialog` `wrapTitle`, used for bot forms only): the 45-character title wraps; the spec asserts it. |
| 10. 5×5 rows lose their grouping | **Fixed:** gap 12 px between rows, 6 px inside (asserted). |
| 11. Smaller | The `title`-only select placeholder and the WebKit modal Tab order are unchanged (minor, as before). |

## The changed BOT tag, everywhere
`BotBadge` is now `bg-mnema-band` (#143D2E) with `text-mnema-text` (#E6EAE8) and a mint border, bold. It measured `rgb(230,234,232) on rgb(20,61,46)` everywhere in both browsers: high contrast, and still clear at the offline member row's 75 % opacity. It is calm and fits the app's green band. Seen in:
- message header (`$R2/chromium-tag-message-and-members.png`);
- member list online and offline (`$R2/*-tag-offline-member.png`);
- profile card (`-tag-profile.png`);
- mention suggestions (`-tag-mention-suggestions.png`);
- reply preview (`-tag-reply-preview.png`);
- audit log (`-tag-audit.png`);
- admin bots list (`-tag-admin-bots.png`).

The app has one theme, so there are no theme variants to check. One inconsistency: the admin bots list and the confirm screen use their own span with `text-mnema-mint` on the band instead of `BotBadge` (light text), so the tag is mint-on-green there and light-on-green elsewhere. Use `BotBadge` in both places.

## Gates (2edd1175b)
| Check | Result |
|---|---|
| `npm run lint` | exit 0 (23 warnings, older) |
| `npm run typecheck`, e2e typecheck | exit 0 |
| `vitest related` for the touched web sources | 425 files, 9772 passed, exit 0 |
| New specs and a11y, Chromium | 7/7 passed |
| New specs and a11y, WebKit | 6/7 passed (layout 390 fails, see above) |

## Not checked (round 2)
- Firefox.
- A real screen reader (the status line's `role=status` was checked in the DOM).
- Touch hardware.
- The server-side commits (8d49554e4, b4a09f8f2, 9d5c8a665 and others) beyond what the UI exercised.

The worktrees are removed, my stacks and the review bot are down, and nothing was committed or pushed.
