import { webcrypto } from 'node:crypto'
import { afterEach, afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import * as mls from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import { DEFAULT_TIMING, type MediaTiming } from './bounds'
import { mediaTiming, resolveTiming } from './lifetime'
import { openFrame, ReplayWindow, sealFrame, type EpochKeys } from './frame'
import { MediaGroup, type MediaStep } from './group'
import { sealHeartbeat } from './heartbeat'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const carolId = fixtureId(3)
const channelId = fixtureId(9)

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })
afterEach(() => { vi.useRealTimers() })

// Short lifetimes; the clock is stepped with setSystemTime and the group asked
// to look at it with maintain(), so the real crypto never waits on fake timers.
const timing: MediaTiming = { rotateMs: 10_000, rotateJitterMs: 2000, absentMs: 4000, heartbeatMs: 1_000_000, maintenanceMs: 1000 }
const START = Date.UTC(2026, 0, 1)

async function open(userId: string, over: Partial<MediaTiming> = {}) {
  const group = await MediaGroup.open(userId, channelId, { ...timing, ...over })
  if (!group) throw new Error('expected a media group')
  return group
}

async function deliver(groups: Map<string, MediaGroup>, from: string, wires: Uint8Array[], roster: string[]) {
  const pending = wires.map(wire => ({ from, wire }))
  while (pending.length) {
    const item = pending.shift()
    if (!item) break
    for (const [id, group] of groups) {
      if (id === item.from) continue
      const step = await group.receive(item.from, item.wire)
      for (const wire of step.wires) pending.push({ from: id, wire })
      const synced = await group.sync(roster)
      for (const wire of synced.wires) pending.push({ from: id, wire })
    }
  }
}

async function pair(over: Partial<MediaTiming> = {}) {
  vi.useFakeTimers({ toFake: ['Date'], now: START })
  const alice = await open(aliceId, over)
  const bob = await open(bobId, over)
  const roster = [aliceId, bobId]
  const groups = new Map([[aliceId, alice], [bobId, bob]])
  await alice.sync([aliceId])
  await deliver(groups, bobId, (await bob.sync(roster)).wires, roster)
  const a = await alice.sync(roster)
  const b = await bob.sync(roster)
  if (a.epoch !== 1 || b.epoch !== 1) throw new Error('expected epoch 1')
  return { alice, bob, groups, roster, a, b }
}

async function roundTrip(sealer: EpochKeys | null, keys: Map<number, EpochKeys>, epoch: number, sender: string) {
  if (!sealer) throw new Error('expected keys')
  const packet = await sealFrame({ keys: sealer, epoch, stream: 1, counter: 0, channelId, senderId: sender, plaintext: new Uint8Array([4, 2]) })
  if (!packet) throw new Error('expected a frame')
  const opened = await openFrame({ keys, channelId, senderId: sender, packet, replay: new ReplayWindow() })
  return opened ? [...opened] : null
}

function kinds(step: MediaStep) {
  return step.wires.map(wire => wire[0] === 0x4d ? 'heartbeat' : mls.decodeMlsMessage(wire, 0)?.[0].wireformat)
}

describe('timing hook', () => {
  it('is the constants unless a test sets values, and ignores values that make no sense', () => {
    expect(DEFAULT_TIMING).toMatchObject({ rotateMs: 1_800_000, rotateJitterMs: 120_000, absentMs: 1_200_000, heartbeatMs: 300_000 })
    expect(mediaTiming()).toBe(DEFAULT_TIMING)
    const hooked = globalThis as { __MNEMA_MEDIA_TIMING__?: unknown }
    hooked.__MNEMA_MEDIA_TIMING__ = { rotateMs: 50, absentMs: -1, heartbeatMs: Number.NaN, maintenanceMs: '5', rotateJitterMs: 0 }
    try {
      expect(mediaTiming()).toEqual({ ...DEFAULT_TIMING, rotateMs: 50, rotateJitterMs: 0 })
    } finally {
      delete hooked.__MNEMA_MEDIA_TIMING__
    }
    // A release build does not allow the hook, whatever the page sets.
    expect(resolveTiming(false, { rotateMs: 1 })).toBe(DEFAULT_TIMING)
    expect(resolveTiming(true, undefined)).toBe(DEFAULT_TIMING)
  })
})

describe('rotation', () => {
  it('advances the epoch with a path commit once the epoch is old enough, only from the committer', async () => {
    const { alice, bob, groups, roster, a, b } = await pair({ absentMs: 1e9 })
    vi.setSystemTime(START + timing.rotateMs - timing.rotateJitterMs - 1)
    expect((await alice.maintain()).wires).toEqual([])
    vi.setSystemTime(START + timing.rotateMs + timing.rotateJitterMs + 1)
    // Bob is not the committer: nothing from him.
    expect((await bob.maintain()).wires).toEqual([])
    const rotated = await alice.maintain()
    expect(kinds(rotated)).toEqual(['mls_public_message'])
    expect(rotated.epoch).toBe(2)
    const decoded = mls.decodeMlsMessage(rotated.wires[0] as Uint8Array, 0)?.[0]
    const content = decoded?.wireformat === 'mls_public_message' ? decoded.publicMessage.content : null
    expect(content?.contentType === 'commit' && content.commit.path).toBeTruthy()
    await deliver(groups, aliceId, rotated.wires, roster)
    const bobNow = await bob.sync(roster)
    expect(bobNow.epoch).toBe(2)
    expect(bobNow.verification?.code).toBe(rotated.verification?.code)
    expect(bobNow.verification?.code).not.toBe(a.verification?.code)
    // Frames open across the switch, in both directions, with the two epochs held.
    const held = new Map([[1, b.key as EpochKeys], [2, bobNow.key as EpochKeys]])
    expect(await roundTrip(a.key, held, 1, aliceId)).toEqual([4, 2])
    expect(await roundTrip(rotated.key, held, 2, aliceId)).toEqual([4, 2])
    expect(await roundTrip(bobNow.key, new Map([[2, rotated.key as EpochKeys]]), 2, bobId)).toEqual([4, 2])
    // The next rotation counts from the new epoch, not from the old one.
    expect((await alice.maintain()).wires).toEqual([])
    await alice.close()
    await bob.close()
  }, 30_000)

  it('draws its delay within the jitter and never rotates a group of one', async () => {
    vi.useFakeTimers({ toFake: ['Date'], now: START })
    const alice = await open(aliceId, { absentMs: 1e9 })
    await alice.sync([aliceId])
    vi.setSystemTime(START + 10 * timing.rotateMs)
    expect((await alice.maintain()).wires).toEqual([])
    const random = vi.spyOn(Math, 'random')
    random.mockReturnValue(0)
    const { alice: a2, bob: b2 } = await pair({ absentMs: 1e9 })
    vi.setSystemTime(START + timing.rotateMs - timing.rotateJitterMs - 1)
    expect((await a2.maintain()).wires).toEqual([])
    vi.setSystemTime(START + timing.rotateMs - timing.rotateJitterMs)
    expect(kinds(await a2.maintain())).toEqual(['mls_public_message'])
    random.mockRestore()
    await Promise.all([alice.close(), a2.close(), b2.close()])
  }, 30_000)

  it('is taken over by the next committer on the age of the epoch it holds', async () => {
    vi.useFakeTimers({ toFake: ['Date'], now: START })
    const alice = await open(aliceId, { absentMs: 1e9 })
    const bob = await open(bobId, { absentMs: 1e9 })
    const carol = await open(carolId, { absentMs: 1e9 })
    const roster = [aliceId, bobId, carolId]
    const groups = new Map([[aliceId, alice], [bobId, bob], [carolId, carol]])
    await alice.sync([aliceId])
    await deliver(groups, bobId, (await bob.sync(roster)).wires, roster)
    await deliver(groups, carolId, (await carol.sync(roster)).wires, roster)
    for (const group of groups.values()) await group.sync(roster)
    // Alice leaves: Bob commits the removal and is the committer from here on.
    vi.setSystemTime(START + 5000)
    const left = [bobId, carolId]
    groups.delete(aliceId)
    await alice.close()
    const removal = await bob.sync(left)
    await deliver(groups, bobId, removal.wires, left)
    const carolNow = await carol.sync(left)
    expect(carolNow.epoch).toBe(removal.epoch)
    // Carol is not the committer and does nothing; Bob rotates when his epoch is old.
    vi.setSystemTime(START + 5000 + timing.rotateMs + timing.rotateJitterMs + 1)
    expect((await carol.maintain()).wires).toEqual([])
    const rotated = await bob.maintain()
    expect(kinds(rotated)).toEqual(['mls_public_message'])
    await deliver(groups, bobId, rotated.wires, left)
    expect((await carol.sync(left)).epoch).toBe(rotated.epoch)
    await Promise.all([bob.close(), carol.close()])
  }, 60_000)
})

describe('absence', () => {
  it('removes a member that showed no sign of life, and lets it back in fresh', async () => {
    const { alice, bob, groups, roster } = await pair()
    const notices: string[] = []
    bob.onNotice(notice => notices.push(notice))
    vi.setSystemTime(START + timing.absentMs - 1)
    expect((await alice.maintain()).wires).toEqual([])
    vi.setSystemTime(START + timing.absentMs + 1)
    const removal = await alice.maintain()
    expect(kinds(removal)).toEqual(['mls_public_message'])
    expect(removal.verification?.members).toEqual([aliceId])
    // Bob gets the commit although still in the room: he is told, and offers again.
    const told = await bob.receive(aliceId, removal.wires[0] as Uint8Array)
    expect(notices).toEqual(['removed'])
    expect(kinds(told)).toEqual(['mls_key_package'])
    await deliver(groups, bobId, told.wires, roster)
    const back = await alice.sync(roster)
    const bobBack = await bob.sync(roster)
    expect(back.verification?.members).toEqual([aliceId, bobId])
    expect(bobBack.epoch).toBe(back.epoch)
    // The returning member starts with a clean slate, not the old silence.
    expect((await alice.maintain()).wires).toEqual([])
    vi.setSystemTime(START + timing.absentMs + 1 + timing.absentMs - 1)
    expect((await alice.maintain()).wires).toEqual([])
    await alice.close()
    await bob.close()
  }, 30_000)

  it('keeps a member that only listens as long as its heartbeats arrive, signed and newer', async () => {
    const { alice, bob } = await pair({ heartbeatMs: 500, rotateMs: 1e9 })
    vi.setSystemTime(START + 3000)
    const beat = await bob.maintain()
    expect(kinds(beat)).toEqual(['heartbeat'])
    const wire = beat.wires[0] as Uint8Array
    expect(wire.byteLength).toBe(78)
    await alice.receive(bobId, wire)
    vi.setSystemTime(START + 6000)
    // 6 s after the join but 3 s after the heartbeat: still here.
    expect((await alice.maintain()).wires.filter(w => w[0] !== 0x4d)).toEqual([])
    // The same heartbeat again, or one claiming to be someone else's, buys nothing.
    await alice.receive(bobId, wire)
    await alice.receive(carolId, wire)
    vi.setSystemTime(START + 7500)
    expect(kinds(await alice.maintain())).toContain('mls_public_message')
    await alice.close()
    await bob.close()
  }, 30_000)

  it('ignores a heartbeat of another epoch, a stranger\'s and a badly signed one', async () => {
    const { alice, bob, b } = await pair({ heartbeatMs: 500, rotateMs: 1e9 })
    vi.setSystemTime(START + 3000)
    const good = (await bob.maintain()).wires[0] as Uint8Array
    const stranger = await webcrypto.subtle.generateKey('Ed25519', true, ['sign', 'verify']) as CryptoKeyPair
    const wrongEpoch = await sealHeartbeat(b.key?.signer as CryptoKey, new Uint8Array(16), bobId, 7, 99)
    const unknown = await sealHeartbeat(stranger.privateKey, new Uint8Array(16), carolId, 1, 99)
    const forged = good.slice()
    forged[forged.length - 1] = (forged[forged.length - 1] ?? 0) ^ 1
    for (const wire of [wrongEpoch, unknown, forged, new Uint8Array([0x4d, 0x48, 1])]) {
      await alice.receive(bobId, wire as Uint8Array)
    }
    vi.setSystemTime(START + timing.absentMs + 1)
    expect(kinds(await alice.maintain())).toContain('mls_public_message')
    await alice.close()
    await bob.close()
  }, 30_000)

  it('counts any handshake from a member as a sign of life', async () => {
    const { alice, bob } = await pair({ rotateMs: 1e9 })
    vi.setSystemTime(START + 3000)
    // A member that is in the group and says anything (here: a reloaded tab's new package) is there.
    const reloaded = await open(bobId)
    await alice.receive(bobId, (await reloaded.sync([aliceId, bobId])).wires[0] as Uint8Array)
    await reloaded.close()
    vi.setSystemTime(START + 6000)
    expect((await alice.maintain()).wires.filter(w => w[0] !== 0x4d)).toEqual([])
    await alice.close()
    await bob.close()
  }, 30_000)

  it('does not remove anyone after its own browser slept', async () => {
    const { alice, bob } = await pair({ rotateMs: 1e9 })
    vi.setSystemTime(START + 10 * 60_000)
    const woke = await alice.maintain()
    expect(kinds(woke)).toEqual(['heartbeat'])
    expect(woke.verification?.members).toEqual([aliceId, bobId])
    await alice.close()
    await bob.close()
  }, 30_000)

  it('sends one heartbeat per interval and none when alone', async () => {
    const { alice, bob } = await pair({ heartbeatMs: 2000, rotateMs: 1e9, absentMs: 1e9 })
    vi.setSystemTime(START + 1999)
    expect((await bob.maintain()).wires).toEqual([])
    vi.setSystemTime(START + 2000)
    expect(kinds(await bob.maintain())).toEqual(['heartbeat'])
    expect((await bob.maintain()).wires).toEqual([])
    await bob.close()
    expect((await bob.maintain()).wires).toEqual([])
    const solo = await open(carolId, { heartbeatMs: 1 })
    expect((await solo.maintain()).wires).toEqual([])
    await solo.sync([carolId])
    vi.setSystemTime(START + 9000)
    expect((await solo.maintain()).wires).toEqual([])
    await solo.close()
    await alice.close()
  }, 30_000)
})

describe('the maintenance timer', () => {
  it('rotates on its own and reports the step, and stops when the group closes', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'], now: START })
    const small = { rotateJitterMs: 0, rotateMs: 5000, maintenanceMs: 1000, heartbeatMs: 1e9, absentMs: 1e9 }
    const alice = await open(aliceId, small)
    const bob = await open(bobId, small)
    const roster = [aliceId, bobId]
    const groups = new Map([[aliceId, alice], [bobId, bob]])
    await alice.sync([aliceId])
    await deliver(groups, bobId, (await bob.sync(roster)).wires, roster)
    await alice.sync(roster)
    await bob.sync(roster)
    const steps: MediaStep[] = []
    alice.onStep(step => steps.push(step))
    await vi.advanceTimersByTimeAsync(5500)
    await vi.waitFor(() => expect(steps.some(step => step.epoch === 2)).toBe(true))
    expect(kinds(steps[0] as MediaStep)).toEqual(['mls_public_message'])
    await alice.close()
    const seen = steps.length
    await vi.advanceTimersByTimeAsync(20_000)
    expect(steps).toHaveLength(seen)
    await bob.close()
  }, 30_000)
})
