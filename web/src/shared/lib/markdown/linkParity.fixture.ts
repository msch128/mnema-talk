// One rule, two implementations: which links a message holds. The server's
// automod reads them with its own pattern (internal/automod/compile.go), the
// client renders them with this Markdown parser. The oracle: every host the
// client would open is a host automod judged, and could read. The vectors are
// shared with internal/automod/linkparity_test.go through a JSON file.
import { renderMarkdown } from '../markdown'

export interface ServerLink { host: string; ok: boolean }
export interface Vector { msg: string; server: ServerLink[] }

export const VECTOR_FILE = 'internal/automod/testdata/link_parity.json'

const ENTITIES: Record<string, string> = { '&amp;': '&', '&lt;': '<', '&gt;': '>', '&quot;': '"', '&#39;': "'" }

/** The href of every link the real renderer emits, as the browser reads the attribute. */
export function renderedHrefs(msg: string): string[] {
  const html = renderMarkdown(msg)
  return [...html.matchAll(/<a href="([^"]*)"/g)].map(m => (m[1] ?? '').replace(/&(?:amp|lt|gt|quot|#39);/g, e => ENTITIES[e] ?? e))
}

// A host a browser can connect to: DNS labels, an IPv4 address or an IPv6
// literal. "allowed.example.}" or "!.x" parse as URL hosts but lead nowhere.
const REACHABLE = /^(?:(?:[a-z0-9_-]+\.)*[a-z0-9_-]+|\[[0-9a-f:.]+\])$/

/** The host the browser connects to for one href; "" for mailto:, what does not parse and what cannot be reached. */
export function openedHost(href: string): string {
  try {
    const url = new URL(href)
    const host = url.hostname.replace(/\.$/, '')
    return (url.protocol === 'http:' || url.protocol === 'https:') && REACHABLE.test(host) ? host : ''
  } catch {
    return ''
  }
}

/**
 * The hosts the client would open that automod did not judge. A server link
 * that could not be read counts for every host (allow list and block-all stop
 * it); the stricter per-mode view is in modeEffects.
 */
export function unjudged(v: Vector): string[] {
  const judged = new Set(v.server.filter(l => l.ok).map(l => l.host))
  return renderedHrefs(v.msg).map(openedHost).filter(h => h !== '' && !judged.has(h))
}

export type Mode = 'block_all' | 'allow_list' | 'block_list'

/** Whether the server's verdict lets the message through, as compile.go matchesLinks decides. */
export function serverAllows(server: readonly ServerLink[], mode: Mode, domains: readonly string[]): boolean {
  const covered = (h: string) => domains.some(d => h === d || h.endsWith('.' + d))
  return !server.some(l => mode === 'block_all' || (mode === 'allow_list' ? !l.ok || !covered(l.host) : l.ok && covered(l.host)))
}

/** Whether a message the client renders shows a link the rule forbids. */
export function clientForbidden(msg: string, mode: Mode, domains: readonly string[]): boolean {
  const covered = (h: string) => domains.some(d => h === d || h.endsWith('.' + d))
  return renderedHrefs(msg).map(openedHost).filter(h => h !== '').some(h => mode === 'block_all' || (mode === 'allow_list' ? !covered(h) : covered(h)))
}

/** The modes in which a forbidden link reaches readers: the server lets it through, the client shows it. */
export function bypassModes(v: Vector): Mode[] {
  const rules: [Mode, string[]][] = [['block_all', []], ['allow_list', ['allowed.example']], ['block_list', ['other.example']]]
  return rules.filter(([mode, d]) => serverAllows(v.server, mode, d) && clientForbidden(v.msg, mode, d)).map(([mode]) => mode)
}

// The generator. A message is a link (scheme, user info, host, port, rest),
// optionally in a Markdown wrapper, between surrounding text. The parts are
// chosen so that every character that is in one stop set and not the other,
// and every character NFKC turns into one, lands at every position.

/** Characters at the edge of the two stop sets, and what NFKC turns into them. */
export const EDGE_CHARS = [
  "'", '"', '`', '<', '>', '\\', ' ', '\t', '\n', '\r', '\f', '\v', '\u0000', '\u0001', '\u001f', '\u007f', '\u0085', '\u00a0',
  '\u2028', '\u3000', '\u200b', '\u00ad', '\u0301', '\u200c', '\u200d', '\u034f', '\u2060', '\ufeff', '\uff02', '\uff07', '\uff1c', '\uff1e', '\uff3c', '\uff40',
  '\ufe64', '\ufe65', '\uff20', '\ufe6b', '\uff0f', '\uff1f', '\uff03', '\uff0e', '\u3002', '\u2024', '\uff1a', '@', '/', '?', '#',
  ':', '%', '|', '*', '_', '~', ')', ']', '}', '(', '[', '.', ',', ';', '!', '&', '='
]

const SCHEMES = ['https://', 'http://', 'HTTPS://', 'hTtP://', 'https:/', 'https:///', 'www.']
const HOSTS = [
  'allowed.example', 'other.example', 'sub.allowed.example', 'allowed.example.other.example', 'other.example.allowed.example',
  'xn--bcher-kva.example', 'b\u00fccher.example', 'ALLOWED.EXAMPLE', 'allowed.example.', '127.0.0.1', '[::1]', '[::1', '2130706433',
  '0x7f.1', 'other%2eexample', '%6fther.example', 'other\uff0eexample', 'other\u3002example', 'oth\u200ber.example', '\u0430llowed.example', 'other.example\u0301'
]
const PORTS = ['', ':80', ':443', ':', ':99999', ':0', ':8o', ':65535']
const RESTS = ['', '/', '/x', '/x?y=1', '#f', '?q', '/a(b)', '/https://other.example', '?u=https://other.example', '\\other.example', '/x.']

/** A small seeded generator (mulberry32), so a failing seed is a repro. */
export function rng(seed: number): () => number {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function pick<T>(r: () => number, xs: readonly T[]): T {
  return xs[Math.floor(r() * xs.length)] as T
}

function userInfo(r: () => number): string {
  switch (Math.floor(r() * 5)) {
    case 0: return ''
    case 1: return 'user@'
    case 2: return pick(r, HOSTS) + pick(r, EDGE_CHARS) + '@'
    case 3: return pick(r, HOSTS) + pick(r, EDGE_CHARS)
    default: return pick(r, HOSTS) + '@'
  }
}

/** One link-like token; edge characters are spliced in at random positions. */
export function linkToken(r: () => number): string {
  let s = pick(r, SCHEMES) + userInfo(r) + pick(r, HOSTS) + pick(r, PORTS) + pick(r, RESTS)
  for (let n = Math.floor(r() * 3); n > 0; n--) {
    const at = Math.floor(r() * (s.length + 1))
    s = s.slice(0, at) + pick(r, EDGE_CHARS) + s.slice(at)
  }
  return s
}

const WRAPS: ((u: string, r: () => number) => string)[] = [
  u => u,
  u => `<${u}>`,
  (u, r) => `[${pick(r, ['x', 'https://allowed.example', 'allowed.example'])}](${u})`,
  u => `[x](<${u}>)`,
  u => `[x](${u} "t")`,
  u => `||${u}||`,
  u => `**${u}**`,
  u => `\`${u}\``,
  u => `\\<${u}>`,
  u => `[x](https://allowed.example) ${u}`,
  (u, r) => `${linkToken(r)} ${u}`
]

/** One generated message. */
export function message(r: () => number): string {
  const before = pick(r, ['', 'see ', 'x', '(', '"', "'", '\u200b', 'a\n'])
  const after = pick(r, ['', ' now', '.', ')', '!', "'", '"', '\n', '||'])
  return before + pick(r, WRAPS)(linkToken(r), r) + after
}

export function messages(seed: number, n: number): string[] {
  const r = rng(seed)
  return Array.from({ length: n }, () => message(r))
}
