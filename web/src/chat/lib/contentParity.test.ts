// One rule, two implementations: what a message may hold. The composer lets a
// message go when its trimmed text is not empty and lengthState is not over,
// and sends the trimmed text (stores/chat/outbox.ts); the server trims with
// Go's white space, refuses NUL and counts runes
// (internal/chat/core/content_parity_test.go writes its verdicts into the
// shared file). Oracle: whatever the composer sends, the server accepts.
// The composer once disagreed on U+0085 and NUL (finding H-L3-11); both are
// fixed in messageText, and every vector must now be accepted. After changing the generator:
//   CONTENT_PARITY_WRITE=1 npx vitest run src/chat/lib/contentParity.test.ts
//   CONTENT_PARITY_UPDATE=1 go test -run TestContentVectorsMatchTheServer ./internal/chat/core
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { createPinia, setActivePinia } from 'pinia'
import { describe, expect, it, vi } from 'vitest'
import { MAX_MESSAGE_LENGTH, lengthState } from './composerLimits'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { postText } from '../../stores/chat/messagePost'
import { messageText } from '../../shared/lib/textLength'
import { rng } from '../../shared/lib/markdown/linkParity.fixture'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('../../shared/lib/api', async (orig) => ({ ...(await orig<object>()), api: apiMock }))

const file = join(import.meta.dirname, '..', '..', '..', '..', 'internal/chat/core/testdata/content_vectors.json')
interface Vector { sent: string; ok: boolean; runes: number }

// White space of one side and not the other, invisible characters, NUL, and
// characters of 1, 2 and 4 UTF-8 bytes or 2 UTF-16 units.
const EDGES = ['', ' ', '\t', '\n', '\u0085', '\u00a0', '\u1680', '\u2028', '\u3000', '\ufeff', '\u200b', '\u180e', '\u0000', '\u0301', '\u00e9', '\u20ac', '😀', '👍🏻']
const FILL = ['a', '\u00e9', '\u20ac', '😀', 'e\u0301']

/** What the composer does with a draft: null when it does not send, else the text it sends. */
export function composerSends(draft: string): string | null {
  const text = messageText(draft)
  if (!text) return null
  if (lengthState(draft)?.over) return null
  return text
}

function drafts(): string[] {
  const r = rng(11)
  const pick = <T>(xs: readonly T[]) => xs[Math.floor(r() * xs.length)] as T
  const out: string[] = []
  for (const a of EDGES) for (const b of EDGES) out.push(a + 'x' + b, a + b)
  for (let i = 0; i < 40; i++) {
    const n = MAX_MESSAGE_LENGTH - 3 + Math.floor(r() * 7)
    const unit = pick(FILL)
    out.push(pick(EDGES) + unit.repeat(Math.ceil(n / Array.from(unit).length)).slice(0, n * unit.length) + pick(EDGES))
  }
  return [...new Set(out)]
}

const sent = () => drafts().map(composerSends).filter((s): s is string => s !== null)
const vectors = (): Vector[] => JSON.parse(readFileSync(file, 'utf8')) as Vector[]

describe('message text: the composer against the server', () => {
  it.runIf(process.env.CONTENT_PARITY_WRITE === '1')('writes the vectors', () => {
    writeFileSync(file, JSON.stringify(sent().map(s => ({ sent: s, ok: false, runes: 0 })), null, 1) + '\n')
  })

  it('the vector file holds what the composer sends', () => {
    expect(vectors().map(v => v.sent)).toEqual(sent())
  })

  it('the server accepts everything the composer sends', () => {
    expect(vectors().filter(v => !v.ok).map(v => JSON.stringify(v.sent.slice(0, 40)))).toEqual([])
  })

  it('a draft of only NUL or only white space of either side sends nothing', () => {
    for (const draft of ['\u0085', '\u0000', ' \u0085\u0000\n', '\u00a0\u3000', '\ufeff', '\ufeff\u0085\ufeff', '\u0000\ufeff\u0000']) expect(composerSends(draft)).toBeNull()
  })

  it('a long run of white space costs nothing', () => {
    const run = 'a' + ' '.repeat(100_000) + 'b'
    const t0 = performance.now()
    expect(messageText(` ${run} `)).toBe(run)
    expect(messageText(' '.repeat(100_000))).toBe('')
    expect(performance.now() - t0).toBeLessThan(5)
  })

  // The store sends what it is given through messageText again; this walks the
  // real request so the composer's verdict and the wire cannot drift apart.
  it('postText puts on the wire exactly what the composer decided to send', async () => {
    setActivePinia(createPinia())
    useServerFeaturesStore().loaded = true
    const wire: string[] = []
    apiMock.mockImplementation((_url: string, opts: { json: { content: string } }) => {
      wire.push(opts.json.content)
      return Promise.resolve({})
    })
    const sends = drafts().map(composerSends).filter((x): x is string => x !== null)
    for (const text of sends) await postText('c', { content: text, parentId: null, replyToId: null, nonce: 'n' }).catch(() => {})
    expect(wire).toEqual(sends)
  })

  it('what the server stores is within the composer limit', () => {
    expect(vectors().filter(v => v.ok && v.runes > MAX_MESSAGE_LENGTH).length).toBe(0)
  })
})
