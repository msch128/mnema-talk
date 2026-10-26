import { webcrypto } from 'node:crypto'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import * as mls from 'ts-mls'
import type { Credential, KeyPackage } from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import { mediaGroupId, mediaUserId } from './bounds'
import { openFrame, ReplayWindow, sealFrame, type EpochKeys } from './frame'
import { MediaGroup, type MediaStep } from './group'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const carolId = fixtureId(3)
const daveId = fixtureId(4)
const channelId = fixtureId(9)

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

async function opened(userId: string) {
  const group = await MediaGroup.open(userId, channelId)
  if (!group) throw new Error('expected a media group')
  return group
}

// With a roster, a receiver also syncs to it after each wire: the call does the
// same, since the roster arrives before the handshake that depends on it.
async function deliver(groups: Map<string, MediaGroup>, from: string, wires: Uint8Array[], roster?: string[], tally?: { wires: number }) {
  const pending = wires.map(wire => ({ from, wire }))
  if (roster) for (const [id, group] of groups) if (roster.includes(id)) group.hint(roster)
  if (tally) tally.wires += wires.length
  const seen = new Set<string>()
  while (pending.length) {
    const item = pending.shift()
    if (!item) break
    const mark = `${item.from}:${item.wire.byteLength}:${item.wire[0]}:${item.wire[item.wire.byteLength - 1]}`
    if (seen.has(mark)) continue
    seen.add(mark)
    for (const [id, group] of groups) {
      if (id === item.from) continue
      const step = await group.receive(item.from, item.wire)
      for (const wire of step.wires) pending.push({ from: id, wire })
      if (tally) tally.wires += step.wires.length
      if (roster?.includes(id)) {
        const synced = await group.sync(roster)
        for (const wire of synced.wires) pending.push({ from: id, wire })
        if (tally) tally.wires += synced.wires.length
      }
    }
  }
}

/** Seals as `sender` with its keys `left` and opens with `right`. */
async function roundTrip(left: EpochKeys | null, right: EpochKeys | null, epoch: number, sender = aliceId) {
  if (!left || !right) throw new Error('expected epoch keys')
  const packet = await sealFrame({ keys: left, epoch, stream: 1, counter: 0, channelId, senderId: sender, plaintext: new Uint8Array([9, 8, 7]), source: 0 })
  if (!packet) throw new Error('expected a sealed frame')
  const opened = await openFrame({ keys: new Map([[epoch, right]]), channelId, senderId: sender, packet, replay: new ReplayWindow(), source: 0 })
  return opened ? [...opened] : null
}


function wireAt(wires: Uint8Array[], index: number): Uint8Array {
  const wire = wires[index]
  if (!wire) throw new Error('expected a handshake wire')
  return wire
}

async function joinedPair() {
  const alice = await opened(aliceId)
  const bob = await opened(bobId)
  const groups = new Map([[aliceId, alice], [bobId, bob]])
  await alice.sync([aliceId])
  const offer = await bob.sync([aliceId, bobId])
  await deliver(groups, bobId, offer.wires, [aliceId, bobId])
  const aliceStep = await alice.sync([aliceId, bobId])
  const bobStep = await bob.sync([aliceId, bobId])
  if (aliceStep.epoch !== 1 || bobStep.epoch !== 1) throw new Error('expected the pair to share epoch 1')
  return { alice, bob, groups, aliceStep, bobStep }
}

async function welcomeFrom(signerId: string, groupId: Uint8Array, joiner: Uint8Array): Promise<Uint8Array> {
  const cs = await mls.getCiphersuiteImpl(mls.getCiphersuiteFromName('MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519'))
  const own = await mls.generateKeyPackage(
    { credentialType: 'basic', identity: new TextEncoder().encode(signerId) },
    mls.defaultCapabilities(), mls.defaultLifetime, [], cs,
  )
  const state = await mls.createGroup(groupId, own.publicPackage, own.privatePackage, [], cs, {
    keyRetentionConfig: mls.defaultKeyRetentionConfig,
    lifetimeConfig: mls.defaultLifetimeConfig,
    keyPackageEqualityConfig: mls.defaultKeyPackageEqualityConfig,
    paddingConfig: mls.defaultPaddingConfig,
    authService: {
      async validateCredential(credential: Credential) {
        if (credential.credentialType !== 'basic') return false
        const text = new TextDecoder().decode(credential.identity)
        return mediaUserId(text) === text
      },
    },
  })
  const decoded = mls.decodeMlsMessage(joiner, 0)
  const message = decoded?.[0]
  if (!message || message.wireformat !== 'mls_key_package') throw new Error('expected a key package')
  const committed = await mls.createCommit(
    { state, cipherSuite: cs },
    { extraProposals: [{ proposalType: 'add', add: { keyPackage: message.keyPackage } }], wireAsPublicMessage: true, ratchetTreeExtension: true },
  )
  if (!committed.welcome) throw new Error('expected a welcome')
  return mls.encodeMlsMessage({ version: 'mls10', wireformat: 'mls_welcome', welcome: committed.welcome })
}

describe('what a flood or a forgery can reset', () => {
  afterEach(() => { vi.useRealTimers() })

  async function ahead() {
    const { alice, bob } = await joinedPair()
    const roster = [aliceId, bobId, carolId, daveId]
    await alice.sync(roster)
    const carol = await opened(carolId)
    const dave = await opened(daveId)
    await alice.receive(carolId, wireAt((await carol.sync(roster)).wires, 0))
    // Bob misses the commit that adds Carol and sees only the one that adds Dave.
    const second = await alice.receive(daveId, wireAt((await dave.sync(roster)).wires, 0))
    return { alice, bob, carol, dave, roster, future: wireAt(second.wires, 1) }
  }

  it('ignores a commit for a later epoch from someone who is not in the group', async () => {
    const { alice, bob, carol, dave, future } = await ahead()
    const before = await bob.sync([aliceId, bobId])
    const forged = await bob.receive(carolId, future)
    expect(forged.wires).toEqual([])
    expect(forged.epoch).toBe(before.epoch)
    expect(forged.key).toBe(before.key)
    const real = await bob.receive(aliceId, future)
    expect(real.key).toBeNull()
    expect(real.wires).toHaveLength(1)
    for (const group of [alice, bob, carol, dave]) await group.close()
  }, 60_000)

  it('resets at most once per window and then waits longer', async () => {
    const { alice, bob, roster, carol, dave } = await ahead()
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'], now: Date.now() })
    const resets: number[] = []
    for (let second = 0; second < 100; second++) {
      if ((await bob.recover(roster)).wires.length) resets.push(second)
      vi.setSystemTime(Date.now() + 1000)
    }
    expect(resets).toEqual([0, 10, 30, 70])
    bob.opened()
    expect((await bob.recover(roster)).wires).toHaveLength(1)
    for (const group of [alice, bob, carol, dave]) await group.close()
  }, 60_000)

  it('tells a member who was removed while still here, and offers again only now and then', async () => {
    const { alice, bob } = await joinedPair()
    const notices: string[] = []
    bob.onNotice(notice => notices.push(notice))
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'], now: Date.now() })
    const roster = [aliceId, bobId]
    const removal = async () => {
      const removed = await alice.sync([aliceId])
      const commit = removed.wires.find(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat === 'mls_public_message')
      const step = await bob.receive(aliceId, commit ?? new Uint8Array())
      // Back in the room: Alice adds whoever offers.
      await alice.sync(roster)
      for (const wire of step.wires) for (const added of (await alice.receive(bobId, wire)).wires) await bob.receive(aliceId, added)
      return step
    }
    expect((await removal()).wires).toHaveLength(1)
    expect(notices).toEqual(['removed'])
    expect((await bob.sync(roster)).epoch).not.toBeNull()
    expect((await removal()).wires).toEqual([])
    expect(notices).toEqual(['removed', 'removed'])
    // The roster still says Bob is here, so he asks the ordinary way and is taken back.
    await alice.sync(roster)
    for (const wire of (await bob.sync(roster)).wires) for (const added of (await alice.receive(bobId, wire)).wires) await bob.receive(aliceId, added)
    expect((await bob.sync(roster)).epoch).not.toBeNull()
    vi.setSystemTime(Date.now() + 11_000)
    expect((await removal()).wires).toHaveLength(1)
    await alice.close()
    await bob.close()
  }, 60_000)
})

describe('a welcome to a member that already holds a group', () => {
  it('is taken by a group of one founded while the welcome was on its way', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const roster = [aliceId, bobId]
    await alice.sync([aliceId])
    const offer = await bob.sync(roster)
    await alice.sync(roster)
    const added = await alice.receive(bobId, wireAt(offer.wires, 0))
    // Bob gave up waiting and founded with the package he had offered.
    const founded = await (bob as unknown as { foundNow(): Promise<MediaStep | null> }).foundNow()
    expect(founded?.epoch).toBe(0)
    for (const wire of added.wires) await bob.receive(aliceId, wire)
    const joined = await bob.sync(roster)
    expect(joined.epoch).toBe(added.epoch)
    expect(joined.verification?.members).toEqual([aliceId, bobId])
    expect(await roundTrip(added.key, joined.key, added.epoch ?? 0)).toEqual([9, 8, 7])
    await alice.close()
    await bob.close()
  }, 30_000)

  it('is taken only from the welcomed group\'s committer', async () => {
    const { alice, bob } = await joinedPair()
    const dave = await opened(daveId)
    const roster = [aliceId, bobId, daveId]
    const offer = await dave.sync(roster)
    await bob.sync(roster)
    // Bob is no committer: Alice, the group's lowest, is. A welcome from him
    // into that group is refused.
    const decoded = mls.decodeMlsMessage(wireAt(offer.wires, 0), 0)
    if (decoded?.[0].wireformat !== 'mls_key_package') throw new Error('expected a key package')
    const bySideDoor = await (bob as unknown as { commit(p: unknown[]): Promise<Uint8Array[]> }).commit([{ proposalType: 'add', add: { keyPackage: decoded[0].keyPackage } }])
    const welcome = bySideDoor.find(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat === 'mls_welcome')
    expect(welcome).toBeDefined()
    expect((await dave.receive(bobId, welcome ?? new Uint8Array())).key).toBeNull()
    for (const group of [alice, bob, dave]) await group.close()
  }, 30_000)

  it('is refused by a group others are in, and from someone outside the group it welcomes to', async () => {
    const { alice, bob, bobStep } = await joinedPair()
    const carol = await opened(carolId)
    const channel = mediaGroupId(channelId)
    if (!channel) throw new Error('expected a group id')
    const roster = [aliceId, bobId, carolId]
    await bob.sync(roster)
    // Carol tries to pull Bob out of the group he shares with Alice.
    const bobPackage = mls.encodeMlsMessage({ version: 'mls10', wireformat: 'mls_key_package', keyPackage: (bob as unknown as { own: { publicPackage: KeyPackage } }).own.publicPackage })
    const hijack = await welcomeFrom(carolId, channel, bobPackage)
    expect((await bob.receive(carolId, hijack)).epoch).toBe(bobStep.epoch)
    // A newcomer takes no welcome claimed by Alice for a group she is not in.
    const fresh = await opened(daveId)
    const freshOffer = await fresh.sync([...roster, daveId])
    const forged = await welcomeFrom(carolId, channel, wireAt(freshOffer.wires, 0))
    expect((await fresh.receive(aliceId, forged)).key).toBeNull()
    for (const group of [alice, bob, carol, fresh]) await group.close()
  }, 30_000)
})

describe('a room that grows, loses a welcome or has a silent lowest member', () => {
  afterEach(() => { vi.useRealTimers() })
  const timers = { toFake: ['Date', 'setTimeout', 'clearTimeout'] as ('Date' | 'setTimeout' | 'clearTimeout')[] }

  async function form(ids: string[]) {
    const groups = new Map<string, MediaGroup>()
    for (const [index, id] of ids.entries()) {
      const group = await opened(id)
      groups.set(id, group)
      const roster = ids.slice(0, index + 1)
      for (const member of roster) await deliver(groups, member, (await (groups.get(member) as MediaGroup).sync(roster)).wires, roster)
    }
    return groups
  }

  async function epochs(groups: Map<string, MediaGroup>, roster: string[]) {
    const seen = new Map<string, { epoch: number | null, key: EpochKeys | null }>()
    for (const [id, group] of groups) {
      const step = await group.sync(roster)
      seen.set(id, { epoch: step.epoch, key: step.key })
    }
    return seen
  }

  it('adds a member between others without anyone starting over', async () => {
    const ids = [1, 2, 3, 4, 6, 7, 8].map(n => fixtureId(n))
    const groups = await form(ids)
    const before = await epochs(groups, ids)
    const everyone = [...ids, fixtureId(5)].sort()
    const joiner = await opened(fixtureId(5))
    groups.set(fixtureId(5), joiner)
    const tally = { wires: 0 }
    const offer = await joiner.sync(everyone)
    // Everyone learns of the new roster first, as the call does.
    for (const id of ids) {
      const step = await (groups.get(id) as MediaGroup).sync(everyone)
      await deliver(groups, id, step.wires, everyone, tally)
    }
    await deliver(groups, fixtureId(5), offer.wires, everyone, tally)
    const after = await epochs(groups, everyone)
    const epoch = after.get(ids[0] as string)?.epoch
    expect(epoch).toBe((before.get(ids[0] as string)?.epoch ?? 0) + 1)
    for (const [, value] of after) expect(value.epoch).toBe(epoch)
    // The joiner's package, the welcome and the commit; nobody else has to speak.
    expect(tally.wires).toBeLessThanOrEqual(5)
    expect(await roundTrip(after.get(ids[0] as string)?.key ?? null, after.get(fixtureId(5))?.key ?? null, epoch ?? 0, ids[0])).toEqual([9, 8, 7])
    for (const group of groups.values()) await group.close()
  }, 120_000)

  it('offers again with a fresh package when the welcome never comes', async () => {
    const alice = await opened(aliceId)
    const bob = await opened(bobId)
    const roster = [aliceId, bobId]
    vi.useFakeTimers({ ...timers, now: Date.now() })
    await alice.sync([aliceId])
    await alice.sync(roster)
    const offer = await bob.sync(roster)
    const lost = await alice.receive(bobId, wireAt(offer.wires, 0))
    expect(lost.wires).toHaveLength(2)
    const steps: MediaStep[] = []
    bob.onStep(step => steps.push(step))
    await vi.advanceTimersByTimeAsync(3000)
    expect(steps).toEqual([])
    await vi.advanceTimersByTimeAsync(1200)
    await vi.waitFor(() => expect(steps).toHaveLength(1))
    const again = wireAt(steps[0]?.wires ?? [], 0)
    expect(again).not.toEqual(wireAt(offer.wires, 0))
    vi.useRealTimers()
    const healed = await alice.receive(bobId, again)
    expect(healed.wires).toHaveLength(2)
    await bob.receive(aliceId, wireAt(healed.wires, 0))
    expect((await bob.receive(aliceId, wireAt(healed.wires, 1))).epoch).toBe(healed.epoch)
    // With a group, it stops asking.
    vi.useFakeTimers({ ...timers, now: Date.now() })
    await vi.advanceTimersByTimeAsync(30_000)
    expect(steps).toHaveLength(1)
    await alice.close()
    await bob.close()
  }, 60_000)

  it('swaps a reloaded committer in at once and only now and then', async () => {
    const { alice, bob } = await joinedPair()
    const roster = [aliceId, bobId]
    // Alice reloads: Bob, the next lowest, swaps her new leaf in himself.
    await alice.close()
    const reloaded = await opened(aliceId)
    const offer = await reloaded.sync(roster)
    expect(offer.epoch).toBeNull()
    const swapped = await bob.receive(aliceId, wireAt(offer.wires, 0))
    expect(swapped.wires.map(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat).sort()).toEqual(['mls_public_message', 'mls_welcome'])
    for (const wire of swapped.wires) await reloaded.receive(bobId, wire)
    const back = await reloaded.sync(roster)
    expect(back.epoch).toBe(swapped.epoch)
    expect(await roundTrip(back.key, swapped.key, back.epoch ?? 0)).toEqual([9, 8, 7])
    // Back in, Alice is the committer again.
    expect((await reloaded.sync([aliceId])).epoch).toBe((back.epoch ?? 0) + 1)
    // Another reload right away is held back by the swap limit.
    await reloaded.close()
    const again = await opened(aliceId)
    const next = await again.sync(roster)
    expect((await bob.receive(aliceId, wireAt(next.wires, 0))).wires).toEqual([])
    await again.close()
    await bob.close()
  }, 60_000)

  it('retries an offer after a removal the limit held back', async () => {
    const { alice, bob } = await joinedPair()
    const roster = [aliceId, bobId]
    vi.useFakeTimers({ ...timers, now: Date.now() })
    const removal = async () => {
      const removed = await alice.sync([aliceId])
      const commit = removed.wires.find(wire => mls.decodeMlsMessage(wire, 0)?.[0].wireformat === 'mls_public_message')
      const step = await bob.receive(aliceId, commit ?? new Uint8Array())
      await alice.sync(roster)
      return step
    }
    const first = await removal()
    for (const wire of first.wires) for (const added of (await alice.receive(bobId, wire)).wires) await bob.receive(aliceId, added)
    const steps: MediaStep[] = []
    bob.onStep(step => steps.push(step))
    expect((await removal()).wires).toEqual([])
    await vi.advanceTimersByTimeAsync(11_000)
    await vi.waitFor(() => expect(steps).toHaveLength(1))
    expect(steps[0]?.wires).toHaveLength(1)
    await alice.close()
    await bob.close()
  }, 60_000)

  it('lets the next lowest member lead when the lowest never speaks', async () => {
    const bob = await opened(bobId)
    const roster = [aliceId, bobId, carolId]
    vi.useFakeTimers({ ...timers, now: Date.now() })
    const steps: MediaStep[] = []
    bob.onStep(step => steps.push(step))
    expect((await bob.sync(roster)).epoch).toBeNull()
    await vi.advanceTimersByTimeAsync(14_000)
    expect(steps.every(step => step.epoch === null)).toBe(true)
    // Alice is left out after 15 s; once nobody let him in for the offer's
    // wait, Bob is the lowest who is here and founds.
    // Key packages are made in real time, so the clock moves in steps.
    await vi.waitFor(async () => {
      await vi.advanceTimersByTimeAsync(2000)
      expect(steps.at(-1)?.epoch).toBe(0)
    }, { timeout: 20_000, interval: 20 })
    // When Alice does arrive, she offers.
    const alice = await opened(aliceId)
    const offer = await alice.sync(roster)
    // Heard from again, she counts: Bob's group of one steps aside for her.
    await bob.receive(aliceId, wireAt(offer.wires, 0))
    const aside = await bob.sync(roster)
    expect(aside.key).toBeNull()
    await alice.close()
    await bob.close()
  }, 60_000)
})
