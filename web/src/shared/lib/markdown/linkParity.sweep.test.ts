// The deep run of the link parity check: generated messages, judged by the
// real automod code (go test, internal/automod/linkparity_test.go) and
// rendered by the real client parser. Needs Go; too slow for every push:
//   LINK_SWEEP=1 [LINK_SWEEP_N=<messages>] [LINK_SHARD=<index>/<count>] [LINK_SWEEP_CLIENT=current] npx vitest run src/shared/lib/markdown/linkParity.sweep.test.ts
// It fails when a message reaches readers with a link a rule forbids. By
// default it renders as the older bundles of installed desktop clients did
// (links with user info and hidden characters in the host, see
// linkParity.test.ts); LINK_SWEEP_CLIENT=current renders with this bundle.
import { execFileSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { describe, expect, it, vi } from 'vitest'
import { bypassModes, messages, unjudged } from './linkParity.fixture'
import type { Vector } from './linkParity.fixture'

vi.mock('./linkAuthority', async original =>
  process.env.LINK_SWEEP_CLIENT === 'current' ? await original() : { plainAuthority: () => true })

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

describe.skipIf(!process.env.LINK_SWEEP)('generated messages against the link rules', () => {
  it('every opened host was judged by automod', () => {
    const [index, count] = shard(process.env.LINK_SHARD ?? '0/1')
    const per = Math.ceil(N / (SEEDS_PER_SHARD * count))
    const through: string[] = []
    const modes = new Map<string, number>()
    let linked = 0
    for (let s = 0; s < SEEDS_PER_SHARD; s++) {
      const seed = index * SEEDS_PER_SHARD + s + 1
      for (const v of judge(messages(seed, per))) {
        if (v.server.length > 0) linked++
        const bypass = bypassModes(v)
        for (const m of bypass) modes.set(m, (modes.get(m) ?? 0) + 1)
        if (bypass.length > 0 && through.length < 40) through.push(`seed ${seed}: ${JSON.stringify(v.msg)} opens ${unjudged(v).join(', ')}; server read ${JSON.stringify(v.server)}; bypasses ${bypass.join(', ')}`)
      }
    }
    console.log(`link parity: shard ${index}/${count}, seeds ${index * SEEDS_PER_SHARD + 1}..${(index + 1) * SEEDS_PER_SHARD}, ${per * SEEDS_PER_SHARD} messages, ${linked} with a link for the server, client ${process.env.LINK_SWEEP_CLIENT ?? 'older bundle'}, bypasses by mode ${JSON.stringify(Object.fromEntries(modes))}`)
    expect(through, 'messages that reach readers with a link a rule forbids').toEqual([])
  }, 3_600_000)
})
