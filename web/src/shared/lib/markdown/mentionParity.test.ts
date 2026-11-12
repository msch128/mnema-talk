// One rule, two implementations: which @names a message mentions. The server
// (internal/mention Names) decides who is notified, the renderer draws the
// pills. The vectors in internal/mention/testdata/parity.json carry the
// server's names (internal/mention/parity_test.go). The check: every name the
// renderer draws as a mention is one the server notifies, and every name the
// server notifies is drawn, except for the documented differences below.
// After changing the generator:
//   MENTION_PARITY_WRITE=1 npx vitest run src/shared/lib/markdown/mentionParity.test.ts
//   MENTION_PARITY_UPDATE=1 go test -run TestMentionParityVectors ./internal/mention
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { renderMarkdown } from '../markdown'
import { rng } from './linkParity.fixture'

const file = join(import.meta.dirname, '..', '..', '..', '..', '..', 'internal/mention/testdata/parity.json')
interface MentionVector { msg: string; server: string[] }

const NAMES = ['max', 'Max', 'a_b', 'old-timer.x', 'all', 'here', 'x', 'ab', 'a'.repeat(32), 'a'.repeat(33), 'b'.repeat(40) + 'c']
const BEFORE = ['', ' ', '(', ',', '.', '*', '_', '~', '|', '"', "'", '`', '\\', '-', 'a', '1', '\n', '[', '>', '/', ':', '\u00a0', '\u3000', '\u200b']
const AFTER = ['', ' ', '.', '..', '-', ',', '!', ')', '*', '`', "'", '\n', 'x', '@']
const WRAPS: ((s: string) => string)[] = [s => s, s => `\`${s}\``, s => '```\n' + s + '\n```', s => `||${s}||`, s => `**${s}**`, s => `> ${s}`, s => `[${s}](https://a.example)`, s => `\\${s}`, s => `# ${s}`, s => `- ${s}`]

function messages(seed: number, n: number): string[] {
  const r = rng(seed)
  const pick = <T>(xs: readonly T[]) => xs[Math.floor(r() * xs.length)] as T
  return Array.from({ length: n }, () => pick(WRAPS)(pick(BEFORE) + '@' + pick(NAMES) + pick(AFTER)))
}

/** The names the renderer draws as user mentions (every name counts as known). */
function drawn(msg: string): string[] {
  const html = renderMarkdown(msg)
  return [...new Set([...html.matchAll(/<span class="md-mention[^"]*">@([^<]*)<\/span>/g)].map(m => (m[1] ?? '').toLowerCase()))]
}

const vectors = (): MentionVector[] => JSON.parse(readFileSync(file, 'utf8')) as MentionVector[]
const SAMPLE = () => messages(1, 1500)

// The differences measured on 2026-11-12 (lane 3, pair 2). They are design
// choices or display-only gaps, not notification defects: the server notifies
// for "@name" after any character that cannot be part of a name, also inside
// code and links, where the renderer draws no pill; "\@name" (an escaped @)
// notifies and draws. A token of 33 to 64 name characters notifies nobody,
// while the renderer draws a pill for its first 32 characters.
function explained(msg: string, server: readonly string[], client: readonly string[]): boolean {
  const extraClient = client.filter(n => !server.includes(n))
  const extraServer = server.filter(n => !client.includes(n))
  const longToken = /@[A-Za-z0-9_.-]{33,}/.test(msg)
  const quiet = msg.includes('`') || /\]\(/.test(msg) || /[^\s(]@/.test(msg) || /^@/.test(msg.replace(/^[#>-] /, ''))
  return (extraClient.length === 0 || longToken) && (extraServer.length === 0 || quiet || longToken)
}

describe('mentions: the server against the renderer', () => {
  it.runIf(process.env.MENTION_PARITY_WRITE === '1')('writes the messages of the vector file', () => {
    writeFileSync(file, JSON.stringify(SAMPLE().map(msg => ({ msg, server: [] })), null, 1) + '\n')
  })

  it('the vector file holds exactly the generated messages', () => {
    expect(vectors().map(v => v.msg)).toEqual(SAMPLE())
  })

  it('every pill the renderer draws notifies, and every notified name is drawn, but for the explained gaps', () => {
    const bad = vectors().filter(v => !explained(v.msg, v.server, drawn(v.msg))).map(v => `${JSON.stringify(v.msg)}: server ${v.server.join(',')} client ${drawn(v.msg).join(',')}`)
    expect(bad).toEqual([])
  })

  it('a pill never stands for a name the server does not notify, outside over-long tokens', () => {
    const bad = vectors().filter(v => !/@[A-Za-z0-9_.-]{33,}/.test(v.msg) && drawn(v.msg).some(n => !v.server.includes(n))).map(v => v.msg)
    expect(bad).toEqual([])
  })
})
