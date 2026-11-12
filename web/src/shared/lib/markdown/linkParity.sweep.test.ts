// The deep run of the link parity check: generated messages, judged by the
// real automod code (go test, internal/automod/linkparity_test.go) and
// rendered by the real client parser. Needs Go; too slow for every push:
//   LINK_SWEEP=1 [LINK_SWEEP_N=<messages>] [LINK_SHARD=<index>/<count>] npx vitest run src/shared/lib/markdown/linkParity.sweep.test.ts
// It fails when a message outside the known classes (linkParity.known.fixture.ts)
// reaches readers with a link automod did not judge, and when a known class no
// longer occurs (the list must shrink).
import { execFileSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { bypassModes, messages, unjudged } from './linkParity.fixture'
import type { Vector } from './linkParity.fixture'
import { KNOWN_CLASSES, knownClass } from './linkParity.known.fixture'

const repoRoot = join(import.meta.dirname, '..', '..', '..', '..', '..')
const N = Number(process.env.LINK_SWEEP_N ?? '100000')
const SEEDS_PER_SHARD = 10

function shard(setting: string): readonly [number, number] {
  const match = /^(\d+)\/(\d+)$/.exec(setting)
  const [index, count] = match ? [Number(match[1]), Number(match[2])] : [Number.NaN, Number.NaN]
  if (!(count >= 1 && index >= 0 && index < count)) throw new Error(`LINK_SHARD must be <index>/<count> with index below count, got "${setting}"`)
  return [index, count]
}

function judge(msgs: string[]): Vector[] {
  const dir = mkdtempSync(join(tmpdir(), 'link-parity-'))
  try {
    const input = join(dir, 'in.json')
    const output = join(dir, 'out.json')
    writeFileSync(input, JSON.stringify(msgs.map(msg => ({ msg, server: [] }))))
    execFileSync('go', ['test', '-count=1', '-run', '^TestLinkParityVectors$', './internal/automod'], {
      cwd: repoRoot, env: { ...process.env, LINK_PARITY_IN: input, LINK_PARITY_OUT: output }, stdio: 'pipe'
    })
    return JSON.parse(readFileSync(output, 'utf8')) as Vector[]
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
}

describe.skipIf(!process.env.LINK_SWEEP)('generated messages against the known classes', () => {
  it('every opened host was judged by automod', () => {
    const [index, count] = shard(process.env.LINK_SHARD ?? '0/1')
    const per = Math.ceil(N / (SEEDS_PER_SHARD * count))
    const regressions: string[] = []
    const seen = new Map<string, number>()
    const modes = new Map<string, number>()
    for (let s = 0; s < SEEDS_PER_SHARD; s++) {
      const seed = index * SEEDS_PER_SHARD + s + 1
      for (const v of judge(messages(seed, per))) {
        const bypass = bypassModes(v)
        if (bypass.length === 0) continue
        for (const m of bypass) modes.set(m, (modes.get(m) ?? 0) + 1)
        const cls = knownClass(v, bypass)
        if (cls) seen.set(cls, (seen.get(cls) ?? 0) + 1)
        else if (regressions.length < 40) regressions.push(`${JSON.stringify(v.msg)} opens ${unjudged(v).join(', ')}; server read ${JSON.stringify(v.server)}; bypasses ${bypass.join(', ')}`)
      }
    }
    console.log(`link parity: shard ${index}/${count}, seeds ${index * SEEDS_PER_SHARD + 1}..${(index + 1) * SEEDS_PER_SHARD}, ${per * SEEDS_PER_SHARD} messages, known classes hit ${JSON.stringify(Object.fromEntries(seen))}, bypasses by mode ${JSON.stringify(Object.fromEntries(modes))}`)
    const healed = KNOWN_CLASSES.map(c => c.name).filter(name => !seen.has(name))
    expect({ regressions, healed }, '"regressions" are outside every known class, "healed" classes no longer occur (delete them)').toEqual({ regressions: [], healed: [] })
  }, 3_600_000)
})
