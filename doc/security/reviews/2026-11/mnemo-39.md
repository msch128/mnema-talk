# Review of talk-mnemo-bot PR #39: attachment hardening (PDF bounds, pixel limit, name escaping, refusal admission, no file retention)

Two rounds on mnema-xyz/talk-mnemo-bot. The first reviewed the five changes as
first pushed; its should-fix findings and one nit were fixed in the same pull
request and checked in the second. Released in Mnemo 0.1.14.

What the pull request fixes (found in the 2026-11-19 audit of the attachment
feature, #22, #29, #37):

- PDF text extraction used lazy regular expressions whose cost grows with the
  square of a hostile input (a 560 KB file of repeated `stream` openers held
  the event loop for about 7 s), inflated streams without an output limit
  (255 KB of deflate became 256 MB), and never cut the extracted text. One
  member's PDF could stop or restart the bot with a provider other than
  Anthropic.
- The text of a consented attachment was kept in the thread history and the
  answer record for 24 hours and sent again with every later question in the
  thread and with Regenerate, without a new consent.
- Refusals of an attachment were posted before the rate limit.
- Images were decoded up to sharp's default of about 268 megapixels.
- Member names went into the Markdown consent prompt unescaped.

## Round 1

**Verdict: MERGE AFTER FIXES.** All five changes improve on main and each
one's tests fail when it is reverted. Three should-fix items.

Run: `npm test` 159 pass, `npm run lint` and `npm run typecheck` clean (the
lint config enforces the 400/800/80/15 limits). Each change reverted on its
own in a scratch copy: its tests fail (old extractor: 40 s for unclosed stream
openers, 1.7 s and 540,000 characters for a 64 MiB stream).

PDF probes up to 10 MB: nothing super-linear; the worst case about 1.5 s per
admitted question (2,527 streams each just over 4 MiB: 1 s; eight 4 MiB
streams of nested `(`: up to 1 s and 417 MB heap before collection).

Name escaping was run through copies of the Talk parser (`blocks.ts`,
`inline.ts`, `rules.ts`, `mentions.ts`): links, autolinks, headings, quotes,
code and spoilers stay text; a code break-out is not possible.

Refusal admission: the mention is admitted once, Yes once; `admit` holds
nothing back. Retention: thread memory and `AnswerRecord.question` hold the
bare question; an answer about a file has no Regenerate button and a forged
one is refused before admission; the consent record holds the message, not
the file, so every use needs a new consent.

Findings:

- S1 (should-fix) `src/bot/consent.ts:32`: an escaped `@all`, `@alice` or
  `<:name:id>` still renders as a mention pill or custom emoji, because the
  renderer's text rules run after escapes resolve. Visual only (no pings).
- S2 (should-fix) `src/bot/file-text.ts:153-160`: strings built one character
  at a time, about 32 bytes of heap per input byte; out of memory at a 96 MB
  heap.
- S3 (should-fix) `src/bot/file-text.ts:29-30`: a stream skipped for its size
  was not charged against the 32 MB total.
- Nits: `/details` did not say a file was sent; tool inputs taken from a file
  stay in admin-only `/details` for 24 h; a message lookup runs before
  admission; the name cut could split an emoji; an unclosed `(` ends the scan
  of its stream; the README named only the 4 MB limit.

Not checked: real-world PDFs beyond the fixtures, Talk's server-side mention
parsing, other renderers, provider-side prompt caching, the integration
tests.

## Round 2 (commits decea7d, 332a3d6, f52b388)

**Verdict: MERGE.**

Run on the final head: `npm test` 169 pass, lint and typecheck clean. Each
round-2 change reverted to the round-1 head in a scratch copy: its tests fail
(7, 2 and 2 tests).

- S1: a word joiner follows each escaped `@`, `<` and `[` (also wiki links);
  the parser copies no longer match the mention, role, custom emoji or wiki
  link rules. Names are cut by whole graphemes; ZWJ stays.
- S2: strings are built from slices with a per-string cap; TJ arrays are
  capped. Nested `(`: 1,019 ms and +478 MB before, 90 ms and +21 MB after; the
  combined worst case 5 to 8 ms with 8 MB heap, and it runs at a 64 MB heap.
- S3: a skipped stream counts as 4 MB; 2,527 over-cap streams take 9 ms.
- `/details` adds "the attached file" only for answers about a file.

Remaining nits: a PDF with eight or more large images before its text loses
the text after them (the README describes the trade-off); the round-1 nits on
`/details` tool inputs, the lookup before admission and an unclosed `(`
are unchanged.
