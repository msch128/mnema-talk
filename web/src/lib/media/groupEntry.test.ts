// When each member of the tree came in, and what the committer makes of it
// before it removes a silent member (groupEntry.ts).
import { describe, expect, it } from 'vitest'
import type { ClientState } from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import type { MediaTiming } from './bounds'
import { Entries, patienceMs } from './groupEntry'

const timing: MediaTiming = { rotateMs: 1e9, rotateJitterMs: 0, absentMs: 30_000, heartbeatMs: 5000, maintenanceMs: 1000 }
// Wait of the committer: the patience (32 s) and one maintenance interval.
const WAIT = 33_000
const [alice, bob, carol] = [fixtureId(1), fixtureId(2), fixtureId(3)]
const none: ReadonlySet<string> = new Set()

/** A tree with these members in it: the leaves of the even nodes, as the code reads them. */
function tree(...ids: string[]): ClientState {
  const ratchetTree: unknown[] = []
  ids.forEach((id, index) => {
    if (index) ratchetTree.push(null)
    ratchetTree.push({ nodeType: 'leaf', leaf: { credential: { credentialType: 'basic', identity: new TextEncoder().encode(id) }, signaturePublicKey: new Uint8Array([index]) } })
  })
  return { ratchetTree } as unknown as ClientState
}

describe('the patience', () => {
  it('is the absence limit and two maintenance intervals, and never below the ten-millisecond floor', () => {
    expect(patienceMs(timing)).toBe(32_000)
    expect(patienceMs({ ...timing, maintenanceMs: 0 })).toBe(30_020)
  })
})

describe('what the committer knows of when members came in', () => {
  it('holds a removal while another member has been in for no more than the patience and an interval', () => {
    const entries = new Entries()
    entries.observe(tree(alice, bob, carol), 0, none)
    expect(entries.holds(new Set([bob]), alice, WAIT, timing)).toBe(true)
    expect(entries.holds(new Set([bob]), alice, WAIT + 1, timing)).toBe(false)
  })

  it('does not wait for the silent ones: a silent newcomer holds nothing', () => {
    const entries = new Entries()
    entries.observe(tree(alice, bob, carol), 0, none)
    expect(entries.holds(new Set([bob, carol]), alice, 1000, timing)).toBe(false)
    expect(entries.holds(new Set([bob]), alice, 1000, timing)).toBe(true)
  })

  it('does not wait for itself: the committer that came in last holds nothing', () => {
    const entries = new Entries()
    entries.observe(tree(alice, bob), 0, none)
    expect(entries.holds(new Set([bob]), alice, 1000, timing)).toBe(false)
    expect(entries.holds(new Set(), alice, 1000, timing)).toBe(true)
  })

  it('forgets a member that left, so that it counts as new when it comes back', () => {
    const entries = new Entries()
    entries.observe(tree(alice, bob, carol), 0, none)
    entries.observe(tree(alice, bob), 50_000, none)
    entries.observe(tree(alice, bob, carol), 100_000, none)
    expect(entries.holds(new Set([bob]), alice, 110_000, timing)).toBe(true)
  })

  it('counts a member whose leaf was swapped as new, even for the same leaf, and not one that only renewed its own', () => {
    const entries = new Entries()
    entries.observe(tree(alice, bob, carol), 0, none)
    // The same tree again (a self-update, or a swap that put the same leaf back).
    entries.observe(tree(alice, bob, carol), 100_000, none)
    expect(entries.holds(new Set([bob]), alice, 110_000, timing)).toBe(false)
    entries.observe(tree(alice, bob, carol), 100_000, new Set([carol]))
    expect(entries.holds(new Set([bob]), alice, 110_000, timing)).toBe(true)
  })

  it('knows nothing of a group that is gone', () => {
    const entries = new Entries()
    entries.observe(tree(alice, bob, carol), 0, none)
    entries.clear()
    expect(entries.holds(new Set(), alice, 1000, timing)).toBe(false)
  })
})
