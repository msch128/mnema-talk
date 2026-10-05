---
name: Mnema Talk
description: A quiet dark room for talking — four tonal grounds, hairline rules, one forest green that means "live", "yours" or "selected".
colors:
  # Dark theme (the only theme the app ships today). Hex values match web/tailwind.config.js (mnema-*).
  canvas: "#0F1110"
  raised: "#171918"
  surface: "#1A1E1C"
  elevated: "#1C1E1D"
  hover: "#202423"
  hairline: "#242826"
  border: "#2B2F2D"
  border-strong: "#3B413E"
  border-field: "#6E7672"
  text: "#E6EAE8"
  text-muted: "#A3B3AA"
  text-tertiary: "#8E9C94"
  body-ink: "#C4C9CE"
  accent: "#2DA771"
  accent-hover: "#36BD81"
  accent-subtle: "#17231D"
  accent-tint: "#1F3A2E"
  accent-ink: "#0A0A0B"
  band: "#143D2E"
  band-mark: "#8CCBAA"
  mint: "#8CCBAA"
  danger: "#F35549"
  danger-ink: "#0A0A0B"
  warning: "#D97706"
  amber: "#FCE4A8"
  amber-ink: "#1F2328"
  scrim: "rgba(0, 0, 0, 0.6)"
  # Light values, used only for docs, README lockups and printed material today.
  light-canvas: "#F2F6F3"
  light-elevated: "#FFFFFF"
  light-text: "#1F2328"
  light-accent: "#0F4C36"
typography:
  font-sans: "Inter, system-ui, -apple-system, BlinkMacSystemFont, sans-serif"
  font-mono: "JetBrains Mono, ui-monospace, SFMono-Regular, monospace"
  title:    { fontSize: "20px", fontWeight: 600, lineHeight: "28px", letterSpacing: "-0.01em" }
  subtitle: { fontSize: "18px", fontWeight: 600, lineHeight: "24px", letterSpacing: "-0.01em" }
  message:  { fontSize: "16px", fontWeight: 400, lineHeight: "22px", tailwind: "text-message" }
  base:     { fontSize: "16px", fontWeight: 400, lineHeight: "24px", tailwind: "text-base" }
  nav:      { fontSize: "15px", fontWeight: 500, lineHeight: "20px", tailwind: "text-nav" }
  small:    { fontSize: "14px", fontWeight: 400, lineHeight: "20px", tailwind: "text-sm" }
  label:    { fontSize: "12px", fontWeight: 500, lineHeight: "16px", tailwind: "text-xs" }
  section:  { fontSize: "12px", fontWeight: 600, letterSpacing: "0.05em", transform: "uppercase", font: "mono" }
  mono:     { fontSize: "12px", fontWeight: 400, lineHeight: "16px" }
rounded:
  sm: "4px"     # badges, pills, inline chips
  md: "6px"     # buttons, inputs, sidebar rows, menu items
  lg: "10px"    # cards, popovers, context menus, toasts
  xl: "14px"    # modals, sheets, voice tiles
  pill: "999px" # avatars, presence dots, unread capsules
spacing:
  unit: "4px"
  row: "32px"          # sidebar row, menu item
  control: "32px"      # desktop control height
  touch: "44px"        # minimum target on touch / coarse pointer
  gutter: "16px"
motion:
  hairspring: "60ms"
  quick: "120ms"
  base: "180ms"
  slow: "220ms"
  ease-spring: "cubic-bezier(0.16, 1, 0.3, 1)"
layout:
  sidebar: { default: "240px", min: "200px", max: "360px" }
  members: { default: "240px", min: "200px", max: "360px" }
  thread:  { default: "400px", min: "320px", max: "640px" }
  chat-min: "400px"
  mobile-breakpoint: "768px"
  phone-reference: "390x844"
---

# Design System: Mnema Talk

Mnema Talk is a small, self-hosted place where a group of friends writes and
talks. The interface follows the Mnema family look — tonal grounds, hairline
rules, one forest green — tuned for a dark, always-open chat and voice app.

This document is the contract for every screen. Tokens live in
`web/tailwind.config.js` (`mnema-*`); a value used in the UI that is not in the
front matter above is either a new token (add it here and in the config in the
same change) or drift.

## Overview

**North star: "a quiet room with the lights low".** The app is open for hours
next to a game or a browser. It must never compete for attention. Depth comes
from four dark grounds stacked a few percent apart, separated by hairlines.
Colour appears only where something is *live* (someone speaking, you are
connected), *yours* (your selection, the active channel, your primary action)
or *needs you* (a mention, an error).

**Mnema, not a chat-app clone.** The reference for every decision is the
Mnema family, not the chat apps people know. Talk borrows no layout tricks,
naming or visual signatures from them: no server rail, no blurple, no "nitro"
glitter, no gamer chrome, no floating hover toolbars stacked on messages, no
coloured username roles. When a pattern exists in both worlds, take the Mnema
form of it: tonal grounds instead of panels, a wash instead of a rail, a mono
micro-label instead of a coloured pill, calm type instead of loud badges.
Smoother and more refined is the goal; familiar-looking is not.

Key characteristics:

- Four-step tonal ground ramp: canvas → raised → surface/elevated → hover.
- Hairline rules instead of boxes; 1px is the default here (Tailwind 3 cannot
  draw 0.5px reliably on every display), 2px means selection or focus.
- One brand colour, Forest `#2DA771`, on well under a tenth of any screen.
- Chat text at 16px for comfortable long reading; chrome at 14–15px.
- Motion on four steps (60/120/180/220ms) with one spring curve, disabled
  under `prefers-reduced-motion`.
- German UI copy, informal *du*, sentence case, no emoji in chrome.

## Colors

### Grounds

| Token | Hex | Role |
|---|---|---|
| `canvas` | `#0F1110` | App ground, chat background, input wells, meter tracks |
| `raised` | `#171918` | Sidebar, member list, thread panel, modal header/footer |
| `surface` | `#1A1E1C` | Inset groups inside panels, hovered list rows on canvas |
| `elevated` | `#1C1E1D` | Modals, popovers, context menus, toasts, cards |
| `hover` | `#202423` | Row hover on `raised`, menu item hover |
| `hairline` | `#242826` | Quiet dividers inside panels and menus |
| `border` | `#2B2F2D` | Panel edges, card edges, modal edge |
| `border-strong` | `#3B413E` | Control edges on hover, pending outlines |
| `border-field` | `#6E7672` | Form control boundary (needs 3:1 against its ground) |

### Ink

| Token | Hex | On canvas | Role |
|---|---|---|---|
| `text` | `#E6EAE8` | 15.6:1 | Headings, names, message text |
| `body-ink` | `#C4C9CE` | 11.6:1 | Long reading text on cards |
| `text-muted` | `#A3B3AA` | 8.7:1 | Secondary text, inactive channels |
| `text-tertiary` | `#8E9C94` | 6.6:1 | Timestamps, hints, placeholders, counts |

`text-tertiary` is the floor for anything a user must read. Never lower its
opacity.

### Forest (the one brand colour)

- **Forest** `#2DA771`: primary buttons, the active channel marker, focus
  ring, the connected-to-voice state, the speaking ring, the mention badge, the
  noise-gate meter above the threshold.
- **Forest pressed** `#36BD81`: hover and pressed fill of Forest controls.
- **Forest ink** `#0A0A0B`: text and icons on a Forest fill. White on Forest is
  only ~3:1 — never `text-white` on `bg-mnema-accent`.
- **Forest wash** `#17231D` and **tint** `#1F3A2E`: selected row ground,
  own-mention highlight behind a message, hover step of the wash.
- **Band** `#143D2E` with **mint** `#8CCBAA`: brand moments only — the login
  card header, the app icon, the logo's outer voice arc.

### Signal colours

- **Danger** `#F35549`: destructive menu items, errors, *disconnect*. Used as
  ink and outline; filled only on the final confirm button of a destructive
  dialog (with `danger-ink`).
- **Warning** `#D97706`: the live meter below the gate threshold ("heard but
  not sent"), degraded connection, reconnecting banner.
- **Amber** `#FCE4A8` with `amber-ink`: search-hit highlight. Theme-fixed
  ground, theme-fixed ink.

### Semantic roles

| Role | Treatment |
|---|---|
| Speaking | 2px Forest ring around the avatar, outside a 2px canvas gap; fades in/out at `quick` |
| Connected to voice | Forest icon + Forest label in the user bar; voice channel row shows participant list |
| Muted (mic) | `MicOff` icon in danger ink; avatar overlay badge on voice tiles |
| Deafened | `HeadphoneOff` icon in danger ink; implies muted |
| Server-muted / banned | Danger outline badge, never a fill |
| Active channel | Forest wash ground, Forest icon, `text` label — no side rail |
| Unread channel | Channel name in `text` weight 600 + a 6px mint dot right-aligned in the row |
| Mention badge | Forest capsule, `accent-ink` digits, mono 11px tabular, min 16px wide, right-aligned in the row |
| Own mention in a message | Forest wash ground across the row, author name in Forest — no rail |
| New-messages divider | `hairline` rule with a centred mono section label in mint: "NEU SEIT 14:02" |
| Meter below threshold | Warning fill |
| Meter above threshold | Forest fill |
| Presence online / idle / offline | 10px dot: Forest / warning / `border-strong`, with a 2px ground-coloured ring |

Rules:

- **One ink rule.** If more than a tenth of a screen is green, demote chrome to
  neutral. Green has to keep meaning "live, yours, selected".
- **Never colour alone.** Every state that uses colour also changes an icon, a
  label or a shape (muted = `MicOff` + red; unread = bold + pill).
- **No gradients, no glows.** Solid fills only. The speaking ring is a solid
  ring, not a glow.

## Typography

Inter does all the work; JetBrains Mono is for things that are literally
codes, counts, keys or timing (invite codes, kbd hints, meter readouts,
section labels, ping in ms).

| Step | Size / line | Weight | Use |
|---|---|---|---|
| Title | 20/28 | 600 | Modal titles, voice stage heading |
| Subtitle | 18/24 | 600 | Card and section headings |
| Message (`text-message`) | 16/22 | 400 | Message body and author names (600) |
| Base (`text-base`) | 16/24 | 400 | Inputs, modal body |
| Nav (`text-nav`) | 15/20 | 500 | Channel and member names |
| Small (`text-sm`) | 14/20 | 400–500 | Buttons, menu items, helper text |
| Label (`text-xs`) | 12/16 | 500 | Timestamps, badges, tooltips |
| Section label | 12, mono, uppercase, +0.05em | 600 | Category headings, settings group labels |

Rules:

- **12px floor.** Nothing in the UI is smaller than `text-xs`.
- **Two uppercase treatments only:** the mono section label, and nothing
  else. Buttons, headings and menu items are sentence case.
- **Tabular numerals** for anything that updates in place or stacks: unread
  counts, meter percentages, ping, upload progress, timestamps in a column.
- **16px inputs on mobile** so iOS does not zoom on focus.

## Layout

Desktop shell, left to right (there is one community, so there is no
server rail):

1. **Sidebar** (`raised`, 240px, resizable 200–360): the Mnema Talk wordmark
   and community name on top, categories with channels, the user bar pinned
   to the bottom.
2. **Main** (`canvas`, min 400px): channel header (name, topic, actions),
   message list, composer. A voice channel shows the **voice stage** here; its
   **talk chat** opens as a right panel without joining.
3. **Thread panel** (`raised`, 400px, resizable 320–640), optional.
4. **Member list** (`raised`, 240px, resizable 200–360), toggleable.

Rows: sidebar rows 32px, 6px radius, 8px horizontal padding, 18px icons;
hover is the `hover` ground, active is the Forest wash.
Messages: 20px horizontal gutter, 32px avatar column, follow-ups by the same
author within 5 minutes collapse under one header. Messages sit directly on
`canvas`, separated by rhythm (8px between groups) rather than rules.

**One brand moment per screen.** As in Mnema, each main view may carry one
band (`band` ground, mint mono micro-label over a `text` title): the voice
stage header and the login card. Text channels use a plain header on
`canvas` with a hairline below.

Routes (every view has a shareable URL; opening it after login lands there):

| Route | View |
|---|---|
| `/c/:channelId` | Text channel |
| `/c/:channelId/m/:messageId` | Text channel, scrolled to and highlighting one message |
| `/c/:channelId/t/:messageId` | Text channel with that thread open |
| `/v/:channelId` | Voice stage, *watching* — not joined until "Beitreten" |
| `/v/:channelId/chat` | Voice stage with the talk chat panel open |
| `/admin/:tab` | Admin panel tab |

Mobile (below 768px, reference 390×844):

- One column. The sidebar becomes a left drawer, the member list a right
  drawer, both over a `scrim`; swipe or the header buttons open them.
- Voice stage tiles stack two per row; the control bar is fixed to the bottom
  with 48px buttons.
- Every target is at least 44×44px on touch or coarse pointers.

## Elevation & depth

Depth is tonal first. In-flow surfaces (rows, cards, panels) never carry a
resting shadow. Shadows are reserved for layers that detach from the page:

| Layer | Shadow |
|---|---|
| Tooltip | `0 4px 12px rgba(0,0,0,0.4)` |
| Context menu, popover | `0 12px 32px rgba(0,0,0,0.5)` + 1px `border` edge |
| Toast | `0 10px 28px rgba(0,0,0,0.5)` |
| Modal / dialog | `0 24px 64px rgba(0,0,0,0.6)` over the `scrim` |

State outlines that appear on hover, selection or speaking use an inset or
outset ring (`box-shadow`), never a border, so nothing shifts by a pixel.

## Shapes

Radius ladder: 4 (badges, chips) · 6 (buttons, inputs, rows, menu items) ·
10 (cards, popovers, menus, toasts) · 14 (modals, voice tiles) · 999 (avatars,
dots, capsule badges only). Containers and buttons never become pills.

Avatars are circles. A voice tile is a 14px-radius rectangle; the speaking
state rings the tile, not just the avatar.

## Components

### Buttons

- Height 32px desktop / 44px touch, 12px horizontal padding, 6px radius,
  14px / 600 label, sentence case.
- **Primary:** Forest fill, Forest-ink label, hover Forest pressed.
- **Secondary:** `elevated` fill, `text` label, 1px `border`; hover `hover`.
- **Ghost:** transparent, `text-muted`; hover `hover` ground + `text`. Default
  for toolbars and row actions.
- **Danger:** transparent, danger ink, 1px danger ring; hover 12% danger wash.
  Filled danger only for the confirm button inside a destructive dialog.
- **Press:** scale 0.98 at `quick` on the spring curve.
- **Focus:** 2px Forest outline, 2px offset, inherits the radius. Visible on
  keyboard focus only (`:focus-visible`).

### Icon buttons and tooltips

- Square 32px (28px inside dense rows, 44px on touch), 18px icon centered.
- **Every icon button has a tooltip and an `aria-label` with the same German
  text.** The tooltip names the action ("Antworten", "Bildschirm teilen") and
  may append the shortcut in mono ("Stummschalten · M").
- Tooltip: `elevated` ground, 1px `border`, 6px radius, 12px / 500 `text`,
  6×8px padding, appears after 400ms hover or immediately on keyboard focus,
  placed above by default with a 6px offset, never covers the hovered control.
- Toggle buttons (mute, deafen, camera, share) use `aria-pressed` and swap
  icon *and* tooltip text ("Stummschalten" ↔ "Stummschaltung aufheben").

### Context menus (right-click)

All actions on a message, channel, category or member live in a context menu.
Right-click, `Shift+F10`, the menu key, or the `⋯` button (touch: long press)
open it.

- Panel: `elevated`, 1px `border`, 10px radius, popover shadow, 6px padding,
  min width 200px, max 280px.
- Item: 32px high, 6px radius, 8px padding, 16px icon left, 14px label,
  optional right-aligned mono shortcut or chevron for a submenu. Hover/focus:
  `hover` ground + `text`.
- **Danger items** (Löschen, Sperren, Entfernen) sit last, after a separator,
  in danger ink with a danger icon; hover is a 12% danger wash.
- Separator: 1px `hairline`, 4px vertical margin.
- Disabled items stay visible at `text-tertiary` with a tooltip explaining why.
- Keyboard: arrows move, `Enter` activates, `Esc` closes and returns focus to
  the invoking element; type-ahead jumps to the first matching item.
- Menus are clipped to the viewport and flip up/left when needed.

Standard menus:

| Target | Items (in order) |
|---|---|
| Message | Antworten · Reaktion hinzufügen · Thread öffnen · Bearbeiten (own) · Text kopieren · Link zur Nachricht kopieren · Als ungelesen markieren — Nachricht löschen (own, admin) |
| Text channel | Als gelesen markieren · Link kopieren · Benachrichtigungen ▸ (Alle / Nur @Erwähnungen / Stumm) · Kanal bearbeiten (admin) — Kanal löschen (admin) |
| Voice channel | Beitreten / Verlassen · Talk-Chat öffnen · Link kopieren · Kanal bearbeiten (admin) — Kanal löschen (admin) |
| Category | Einklappen/Ausklappen · Alle als gelesen markieren · Kanal erstellen (admin) · Kategorie bearbeiten (admin) — Kategorie löschen (admin) |
| Member | Profil · Erwähnen · Lautstärke (slider, voice only) · Lokal stummschalten — Aus Talk entfernen (admin) · Sperren (admin) |

### Message row

- Avatar 32px, author name 16/600 in `text` (never coloured by role), time as
  a mono 12px `text-tertiary` micro-label after the name, body
  `text-message` in `body-ink`.
- Hover: the row takes the `surface` ground (6px radius, inset by 8px from
  the gutter). **No floating toolbar.** Three quiet ghost icon buttons fade in
  *inside* the row, right-aligned on the header line: Reaktion hinzufügen
  (`SmilePlus`), Antworten (`Reply`), Bearbeiten (`Pencil`, own messages
  only), followed by `⋯` for the context menu. Everything else is in the
  context menu.
- The inline actions are reachable by keyboard: focusing a message (Tab /
  arrow keys in the list) reveals them.
- Reply preview: a single quiet line above the message — `Reply` icon,
  author in `text-muted` 500, a one-line excerpt in `text-tertiary`; click
  scrolls to the original and washes it Forest for 1.2s.
- Reactions: 4px-radius chips on `surface` with a hairline ring, mono count;
  your own reaction takes the Forest wash and Forest ink (Mnema's mention
  pill form).
- Edited marker "(bearbeitet)" in `text-xs` `text-tertiary`.

### Composer

`surface` well, 10px radius, 1px `border` (Forest on focus), 16px text, auto
grows to 50% of the viewport. Left: upload button. Right: emoji, send. Above
it: reply bar (`ReplyComposerBar`) and the typing indicator line ("zoe
schreibt …", `text-xs` `text-tertiary`, animated three-dot ellipsis that stops
under reduced motion).

### Badges

- **Unread:** 4×8px white pill on the left edge of the channel row + bold name.
- **Mention count:** Forest capsule, `accent-ink` mono 11px tabular, 16px tall.
- **Role:** `Crown` icon in amber for the admin, never a coloured name.
- **Count chips** (participants, members): mono 12px `text-tertiary`.

### Avatars

Circle, initials on a Forest-wash ground with `mint` ink when no image.
Presence dot bottom-right (10px + 2px ring in the parent's ground). Speaking:
2px Forest ring outside a 2px gap. Sizes: 24 (inline), 32 (lists and messages), 40
(user bar), 80 (profile), 96+ (voice tile without video).

### Voice tiles

Tiles are Mnema cards: `elevated` ground, 14px radius, hairline ring, no
shadow, 16:9. Without video the tile shows a 96px avatar centred over the
ground and the name below it as a `text-nav` label — not a dark box with a
corner chip. With video (camera `cover`, screen `contain`) the name and status
sit in a mono micro-label strip along the bottom on a `canvas` ground at 80%.
Speaking: 2px Forest ring on the tile edge. A screenshare can be focused to
fill the stage; the other tiles become a row of 120px cards below it.

Control bar: a `raised` strip under the stage with a hairline top edge (not a
floating pill), ghost icon buttons Mikrofon · Kopfhörer · Kamera · Bildschirm
teilen, then a danger-outlined "Verlassen" button with label. Each has a
tooltip and `aria-pressed`.

**Watching state** (opened via link, not joined): participants visible,
own tile absent, a primary "Beitreten" button centered under the tiles, the
control bar replaced by "Talk-Chat öffnen".

### Level meter

The input meter always shows the **raw live microphone level**, independent
of the noise gate and mute — users calibrate against it.

- Track: `canvas`, 1px `border`, 6px radius, 24px high.
- Fill: warning below the threshold, Forest at or above it; width animates at
  `hairspring`.
- Threshold marker: 4px white bar, draggable on the meter itself (and mirrored
  by the range slider below for keyboard users).
- Peak marker: 2px line holding the loudest level for 1.5s, amber below /
  Forest-pressed above the threshold.
- Readout: mono 12px tabular, "Pegel 42 % · Spitze 58 % · Schwelle 32 %".

### Toasts and banners

- **Toast** (bottom-right desktop, top on mobile): `elevated`, 10px radius,
  toast shadow, 16px icon, 14px text, optional single action ("Erneut
  versuchen"), auto-dismiss after 5s unless it has an action; errors stay until
  dismissed. Replaces every `alert()`.
- **Banner** (top of main, full width): connection state. Warning ground wash
  + warning icon "Verbindung getrennt – verbinde neu …" with a live retry
  counter; turns Forest "Wieder verbunden" for 2s, then hides.

### Modals and dialogs

- `elevated`, 14px radius, 1px `border`, dialog shadow, over `scrim`.
- Header (`raised`) with title + close icon button, body 24px padding, footer
  (`raised`) with actions right-aligned: secondary then primary.
- `role="dialog"`, `aria-modal="true"`, labelled by the title; focus moves to
  the first field (or the safe button), is trapped inside, `Esc` closes,
  focus returns to the opener.
- **Destructive confirm:** names the object ("#allgemein löschen?"), explains
  the consequence in one line, the default focused button is "Abbrechen", the
  confirm is filled danger.

### Forms

Inputs 32px (44px touch), `canvas` well, 1px `border-field`, 6px radius, Forest
border on focus, danger border + message under the field when invalid
(`aria-invalid`, `aria-describedby`). Labels above, `text-sm` 500 `text-muted`.

## Iconography

- Library: **Lucide** via `@lucide/vue`. No other icon set, no emoji as icons.
- Sizes: 16px in menus and inline, 18px in sidebar rows and icon buttons,
  20px in the voice control bar. Stroke 2 (Lucide default); 1.75 is allowed at
  20px+ for a lighter bar.
- Icons inherit `currentColor`; colour follows the semantic roles above.
- Filled icons only for status (presence dot, live indicator).

Action → icon mapping (keep consistent across buttons, menus and tooltips):

| Action | Icon |
|---|---|
| Text channel | `Hash` |
| Voice channel | `Volume2` |
| Reaktion hinzufügen | `SmilePlus` |
| Antworten | `Reply` |
| Thread öffnen | `MessagesSquare` |
| Bearbeiten | `Pencil` |
| Löschen | `Trash2` |
| Text kopieren / Link kopieren | `Copy` / `Link` |
| Als ungelesen / gelesen markieren | `MailOpen` / `CheckCheck` |
| Benachrichtigungen | `Bell` / `BellOff` |
| Erwähnen | `AtSign` |
| Mikrofon an / aus | `Mic` / `MicOff` |
| Kopfhörer an / taub | `Headphones` / `HeadphoneOff` |
| Kamera an / aus | `Video` / `VideoOff` |
| Bildschirm teilen | `Monitor` / `MonitorOff` |
| Verlassen | `PhoneOff` |
| Audio-Einstellungen | `Sliders` |
| Push-to-Talk | `Radio` |
| Verbindungsstatistik | `Activity` |
| Hochladen / Bild | `Plus` / `Image` |
| Kanal erstellen / Kategorie erstellen | `Plus` / `FolderPlus` |
| Einladen | `UserPlus` |
| Admin | `ShieldCheck`, admin badge `Crown` |
| Sperren | `Ban` |
| Suche | `Search` |
| Schließen | `X` |
| Mehr | `MoreHorizontal` |
| Abmelden | `LogOut` |
| Fehler / Hinweis | `AlertCircle` / `HelpCircle` |
| Erneut versuchen | `RefreshCw` |

## Logo

Files in `web/public/` (copies in `docs/brand/`):

| File | Use |
|---|---|
| `mnema-talk-mark.svg` | Mark on dark grounds (app header, login) |
| `mnema-talk-mark-mono.svg` | Single colour (print, embossing) |
| `mnema-talk-wordmark-dark.svg` | Mark + "Mnema Talk" on dark grounds |
| `mnema-talk-wordmark-light.svg` | Mark + "Mnema Talk" on light grounds (README, docs) |
| `favicon.svg` | Browser tab, small icons (simplified, one voice arc) |
| `mnema-talk-icon-maskable.svg` | PWA / home-screen icon (full bleed, safe zone) |

The mark is the Mnema node figure — a filled centre node joined to three
outline nodes — with two arcs radiating from the centre: the conversation
leaving the room. The inner arc is Forest, the outer arc mint.

Rules: use the SVG files, never redraw. Minimum mark 16px (use `favicon.svg`
below 24px); minimum lockup 120px wide. Clear space on every side at least one
outer node's diameter. Only the sanctioned colourways above; no rotation, no
effects, no gradients.

## Copy & tone

German, informal *du*, short, calm. The app is a friend's living room, not a
product launch.

- **Sentence case** everywhere; no exclamation marks in UI copy.
- **Verbs on buttons:** "Beitreten", "Senden", "Kanal erstellen" — never "OK".
- **Empty states** say what will appear and how: "Noch keine Nachrichten.
  Schreib die erste." / "Niemand im Talk. Klick auf Beitreten."
- **Loading:** skeleton rows; if text is needed: "Lädt …".
- **Errors** are specific and offer the next step: "Datei ist zu groß (max.
  50 MB)." / "Server nicht erreichbar. Erneut versuchen" — never "Ein Fehler
  ist aufgetreten" and never blame the user.
- **Confirmations** in past tense, one or two words: "Gespeichert",
  "Link kopiert".
- **Destructive** dialogs name the object: "Nachricht löschen?", "#musik
  löschen?". Default focus on "Abbrechen".
- No implementation details in the UI ("S3", "SFU", "WebSocket") except on the
  connection-statistics screen.

## Do's and Don'ts

Do:

- Check every new screen against Mnema first: would it sit naturally next to
  the Mnema family, quiet and tonal?
- Separate surfaces with the ground ramp and a hairline before reaching for a
  shadow.
- Keep Forest rare: live, yours, selected, focus, mention.
- Give every icon button a tooltip and a matching `aria-label`.
- Put every action of an object in its context menu, even when it also has a
  button.
- Use rings (`box-shadow`) for state outlines so layout never shifts.
- Use `accent-ink` on Forest, `danger-ink` on filled danger, `amber-ink` on
  amber.
- Use tabular numerals for counts, meters and timestamps.
- Respect `prefers-reduced-motion` (no pulsing, no animated ellipsis, instant
  transitions).
- Ship 44px targets and 16px inputs on touch.

Don't:

- Copy other chat apps' signatures: server rails, floating hover toolbars,
  coloured role names, pill-shaped unread markers on the left edge, red "new"
  bars, gamer-style badges or their vocabulary ("Server boosten",
  "Nitro"). Take the Mnema form instead.
- Add a second brand colour, a gradient or a glow.
- Use `alert()`, `confirm()` or `prompt()`.
- Show more than three inline actions on a message.
- Use emoji as icons or in system copy (user content may contain any).
- Use white text on Forest, or lower the opacity of `text-tertiary`.
- Put resting shadows on rows, cards or panels.
- Title Case buttons or add a new uppercase style.
- Show internal technology names to users.
