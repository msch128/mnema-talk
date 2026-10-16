import { webcrypto } from 'node:crypto'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { fixtureId } from '../../test-fixtures.fixture'
import { HANDSHAKE_MAX_BYTES } from './bounds'
import type { FrameStats, MediaGuard } from './guard'
import { browserMediaCall, mediaSettled } from './browserCall'
import { MediaGroup } from './group'
import { openFrame, ReplayWindow, sealFrame, type EpochKeys } from './frame'
import type { MediaCall } from './api'
import type { MediaVerification } from './verify'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const carolId = fixtureId(3)
const lowIdFor = '00000000-0000-4000-8000-000000000001'
const channelId = fixtureId(9)

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

function recordingGuard(): MediaGuard & { keys: { epoch: number, key: EpochKeys, replace: boolean }[], sends: string[], receives: string[], stats(senders: Record<string, FrameStats>): void } {
  const keys: { epoch: number, key: EpochKeys, replace: boolean }[] = []
  const sends: string[] = []
  const receives: string[] = []
  let statsListener: (senders: Record<string, FrameStats>) => void = () => {}
  return {
    stats: senders => statsListener(senders),
    onStats(listener) { statsListener = listener },
    keys,
    sends,
    receives,
    armSender(_sender, channelId, senderId) {
      sends.push(`${channelId}:${senderId}`)
      return true
    },
    armReceiver(_receiver, channelId, senderId) {
      receives.push(`${channelId}:${senderId}`)
      return true
    },
    setKey(epoch, key, replace = false) { keys.push({ epoch, key, replace }) },
    clearKeys() { keys.length = 0 },
    reset() { keys.length = 0 },
  }
}

describe('browser media call', () => {
  it('ignores a channel that is not a member id and a receiver with no sender', () => {
    const guard = recordingGuard()
    const call = browserMediaCall(guard)
    call.start('room', 'person')
    call.roster(['person'])
    call.receive('person', '@@@@')
    call.receive(bobId, '')
    call.receive(bobId, 'a'.repeat(48_001))
    const sender = { transform: null } as RTCRtpSender
    const receiver = { transform: null } as RTCRtpReceiver
    expect(call.protectSender(sender)).toBe(false)
    expect(call.protectIncoming(undefined, aliceId)).toBe(false)
    expect(call.protectIncoming(receiver, 'alice')).toBe(false)
    expect(call.prepareIncoming(undefined as unknown as RTCRtpReceiver)).toBe(false)
    call.stop()
    expect(call.prepareIncoming(receiver)).toBe(false)
    expect(guard.sends).toEqual([])
  })

  it('arms a receiver for the channel before any publisher is named', () => {
    const guard = recordingGuard()
    const call = browserMediaCall(guard)
    const receiver = { transform: null } as RTCRtpReceiver
    call.start(channelId, aliceId)
    expect(call.prepareIncoming(receiver)).toBe(true)
    expect(call.protectIncoming(receiver, bobId)).toBe(true)
    expect(guard.receives).toEqual([`${channelId}:null`, `${channelId}:${bobId}`])
    call.stop()
  })

  it('closes the previous group when the channel changes', async () => {
    const guard = recordingGuard()
    const call = browserMediaCall(guard)
    const nextChannel = fixtureId(8)
    call.start(channelId, aliceId)
    await mediaSettled(call)
    const close = vi.spyOn(MediaGroup.prototype, 'close')
    call.start(nextChannel, aliceId)
    await mediaSettled(call)
    expect(close).toHaveBeenCalled()
    const sender = { transform: null } as RTCRtpSender
    expect(call.protectSender(sender)).toBe(true)
    expect(guard.sends).toEqual([`${nextChannel}:${aliceId}`])
    close.mockRestore()
    call.stop()
    await mediaSettled(call)
  })

  it('gives both members the same epoch key and then forgets it on leave', async () => {
    const aliceGuard = recordingGuard()
    const bobGuard = recordingGuard()
    const alice = browserMediaCall(aliceGuard)
    const bob = browserMediaCall(bobGuard)
    const seen: number[] = []
    alice.onKey(() => { seen.push(aliceGuard.keys.at(-1)?.epoch ?? -1) })
    const aliceCodes: (MediaVerification | null)[] = []
    const bobCodes: (MediaVerification | null)[] = []
    alice.onVerification(verification => aliceCodes.push(verification))
    bob.onVerification(verification => bobCodes.push(verification))
    alice.onWire((_channel, wire) => { bob.receive(aliceId, wire) })
    bob.onWire((_channel, wire) => { alice.receive(bobId, wire) })
    alice.start(channelId, aliceId)
    bob.start(channelId, bobId)
    await mediaSettled(alice)
    await mediaSettled(bob)
    alice.roster([aliceId, bobId])
    bob.roster([bobId, aliceId, 'not-a-member'])
    for (let turn = 0; turn < 8; turn++) {
      await mediaSettled(alice)
      await mediaSettled(bob)
    }
    const aliceKey = aliceGuard.keys.at(-1)
    const bobKey = bobGuard.keys.at(-1)
    if (!aliceKey || !bobKey) throw new Error('expected both epoch keys')
    expect(aliceKey.epoch).toBe(bobKey.epoch)
    expect(seen).toContain(aliceKey.epoch)
    // Each code is reported once, and both ends end up on the same one.
    expect(aliceCodes.at(-1)).toEqual(bobCodes.at(-1))
    expect(aliceCodes.at(-1)).toMatchObject({ epoch: aliceKey.epoch, members: [aliceId, bobId] })
    expect(new Set(aliceCodes.map(code => code?.code ?? null)).size).toBe(aliceCodes.length)
    const packet = await sealFrame({
      keys: aliceKey.key, epoch: aliceKey.epoch, stream: 1, counter: 0, channelId, senderId: aliceId, plaintext: new Uint8Array([1, 2]),
    })
    if (!packet) throw new Error('expected a sealed frame')
    const bobKeys = new Map([[bobKey.epoch, bobKey.key]])
    await expect(openFrame({ keys: bobKeys, channelId, senderId: aliceId, packet, replay: new ReplayWindow() })).resolves.toEqual(new Uint8Array([1, 2]))
    // Alice's MLS leaf key signed it: bob cannot pass it off as his, nor alice as bob.
    await expect(openFrame({ keys: bobKeys, channelId, senderId: bobId, packet, replay: new ReplayWindow() })).resolves.toBeNull()
    const forged = await sealFrame({
      keys: bobKey.key, epoch: bobKey.epoch, stream: 1, counter: 0, channelId, senderId: aliceId, plaintext: new Uint8Array([6]),
    })
    if (!forged) throw new Error('expected a sealed frame')
    await expect(openFrame({ keys: new Map([[aliceKey.epoch, aliceKey.key]]), channelId, senderId: aliceId, packet: forged, replay: new ReplayWindow() })).resolves.toBeNull()
    const sender = { transform: null } as RTCRtpSender
    const receiver = { transform: null } as RTCRtpReceiver
    expect(alice.protectSender(sender)).toBe(true)
    expect(alice.protectIncoming(receiver, bobId)).toBe(true)
    expect(aliceGuard.sends).toEqual([`${channelId}:${aliceId}`])
    expect(aliceGuard.receives).toEqual([`${channelId}:${bobId}`])
    alice.receive(bobId, '@@@@')
    alice.stop()
    bob.stop()
    await mediaSettled(alice)
    await mediaSettled(bob)
    expect(alice.protectSender(sender)).toBe(false)
    expect(aliceGuard.keys).toEqual([])
    expect(aliceCodes.at(-1)).toBeNull()
  }, 30_000)

  it('forgets a discarded epoch before a later group installs its own', async () => {
    const guard = recordingGuard()
    const bob = browserMediaCall(guard)
    bob.start(channelId, bobId)
    bob.roster([aliceId, bobId])
    await mediaSettled(bob)
    expect(guard.keys).toEqual([])
    bob.roster([bobId])
    await mediaSettled(bob)
    expect(guard.keys.map(item => item.epoch)).toEqual([0])
    bob.roster([aliceId, bobId])
    await mediaSettled(bob)
    expect(guard.keys).toEqual([])
    bob.stop()
    await mediaSettled(bob)
  }, 30_000)

  it('repeats a missing key package when the call resumes', async () => {
    const bob = browserMediaCall(recordingGuard())
    const wires: string[] = []
    bob.onWire((_channel, wire) => { wires.push(wire) })
    bob.start(channelId, bobId)
    bob.roster([aliceId, bobId])
    await mediaSettled(bob)
    expect(wires).toHaveLength(1)
    bob.roster([aliceId, bobId])
    await mediaSettled(bob)
    expect(wires).toHaveLength(1)
    bob.resume()
    await mediaSettled(bob)
    expect(wires).toHaveLength(2)
    bob.stop()
    await mediaSettled(bob)
  }, 30_000)

  it('installs a new key when the epoch number does not change', async () => {
    const guard = recordingGuard()
    const call = browserMediaCall(guard)
    call.start(channelId, aliceId)
    call.roster([aliceId])
    await mediaSettled(call)
    const current = guard.keys.at(-1)
    if (!current) throw new Error('expected an epoch key')
    const replacement = { ...current.key, key: await crypto.subtle.generateKey({ name: 'AES-GCM', length: 128 }, false, ['encrypt', 'decrypt']) }
    const receive = vi.spyOn(MediaGroup.prototype, 'receive')
    receive.mockResolvedValueOnce({ wires: [], epoch: current.epoch, key: current.key })
    call.receive(bobId, btoa('same'))
    await mediaSettled(call)
    expect(guard.keys).toHaveLength(1)
    receive.mockResolvedValueOnce({ wires: [], epoch: current.epoch, key: replacement })
    call.receive(bobId, btoa('next'))
    await mediaSettled(call)
    receive.mockRestore()
    expect(guard.keys.at(-1)).toEqual({ epoch: current.epoch, key: replacement, replace: true })
    call.stop()
    await mediaSettled(call)
  }, 30_000)

  it('settles a call it does not own', async () => {
    await expect(mediaSettled({} as MediaCall)).resolves.toBeUndefined()
  })

  it('drops a group that lost the race and a handshake the room cannot carry', async () => {
    const guard = recordingGuard()
    const call = browserMediaCall(guard)
    const wires: string[] = []
    call.onWire((_channel, wire) => { wires.push(wire) })
    const close = vi.spyOn(MediaGroup.prototype, 'close')
    const nextChannel = fixtureId(8)
    call.start(channelId, aliceId)
    call.start(nextChannel, aliceId)
    await mediaSettled(call)
    expect(close).toHaveBeenCalled()
    close.mockRestore()
    const sender = { transform: null } as RTCRtpSender
    expect(call.protectSender(sender)).toBe(true)
    expect(guard.sends).toEqual([`${nextChannel}:${aliceId}`])
    const sync = vi.spyOn(MediaGroup.prototype, 'sync').mockResolvedValueOnce({ wires: [new Uint8Array()], epoch: null, key: null })
    call.roster([aliceId, bobId])
    await mediaSettled(call)
    sync.mockRestore()
    expect(wires).toEqual([])
    call.receive(bobId, '')
    call.receive(bobId, 'a'.repeat(48_001))
    call.receive(bobId, '@@@@')
    let binary = ''
    for (const byte of new Uint8Array(HANDSHAKE_MAX_BYTES + 1)) binary += String.fromCharCode(byte)
    call.receive(bobId, btoa(binary))
    await mediaSettled(call)
    call.stop()
    await mediaSettled(call)
    expect(call.protectSender(sender)).toBe(false)
  }, 30_000)

  it('continues after a failed sync and stops publishing when the call ends mid-handshake', async () => {
    const guard = recordingGuard()
    const call = browserMediaCall(guard)
    const sync = vi.spyOn(MediaGroup.prototype, 'sync').mockRejectedValueOnce(new Error('sync failed'))
    call.start(channelId, aliceId)
    await mediaSettled(call)
    sync.mockRestore()
    call.roster([aliceId])
    await mediaSettled(call)
    const sender = { transform: null } as RTCRtpSender
    expect(call.protectSender(sender)).toBe(true)
    const receive = MediaGroup.prototype.receive
    const spy = vi.spyOn(MediaGroup.prototype, 'receive').mockImplementation(function (this: MediaGroup, from: string, wire: Uint8Array) {
      spy.mockRestore()
      call.stop()
      return receive.call(this, from, wire)
    })
    call.receive(bobId, btoa('hi'))
    await mediaSettled(call)
    expect(call.protectSender(sender)).toBe(false)
  }, 30_000)

  describe('frames of an epoch the call has no key for', () => {
    afterEach(() => { vi.useRealTimers() })
    const newer = (epoch: number): FrameStats => ({ opened: 0, newer: 5, epoch })
    const opens: FrameStats = { opened: 5, newer: 0, epoch: -1 }
    const lowId = '00000000-0000-4000-8000-000000000001'

    async function waitingJoiner(roster: string[]) {
      const guard = recordingGuard()
      const bob = browserMediaCall(guard)
      const wires: string[] = []
      bob.onWire((_channel, wire) => { wires.push(wire) })
      bob.start(channelId, bobId)
      bob.roster(roster)
      await mediaSettled(bob)
      expect(wires).toHaveLength(1)
      vi.useFakeTimers({ toFake: ['Date'], now: Date.now() })
      return { guard, bob, wires }
    }

    async function later(bob: MediaCall, ms: number) {
      vi.setSystemTime(Date.now() + ms)
      await mediaSettled(bob)
    }

    it('does nothing for one publisher with a made-up epoch while another opens', async () => {
      const { guard, bob, wires } = await waitingJoiner([aliceId, bobId, carolId])
      for (let second = 0; second < 12; second++) {
        guard.stats({ [carolId]: newer(9), [aliceId]: opens })
        await later(bob, 1000)
      }
      expect(wires).toHaveLength(1)
      bob.stop()
    }, 30_000)

    it('does nothing for a single publisher that is not the committer', async () => {
      const { guard, bob, wires } = await waitingJoiner([aliceId, bobId, carolId])
      for (let second = 0; second < 12; second++) {
        guard.stats({ [carolId]: newer(9) })
        await later(bob, 1000)
      }
      expect(wires).toHaveLength(1)
      bob.stop()
    }, 30_000)

    it('starts over after five seconds when every publisher is on a newer epoch', async () => {
      const { guard, bob, wires } = await waitingJoiner([aliceId, bobId, carolId])
      for (let second = 0; second < 5; second++) {
        guard.stats({ [carolId]: newer(9), [aliceId]: newer(9) })
        await later(bob, 1000)
      }
      expect(wires).toHaveLength(1)
      guard.stats({ [carolId]: newer(9), [aliceId]: newer(9) })
      await later(bob, 1000)
      await vi.waitFor(() => expect(wires).toHaveLength(2))
      expect(wires[1]).not.toBe(wires[0])
      bob.stop()
    }, 30_000)

    it('wants the committer on a newer epoch for the whole window, not one frame after a quiet spell', async () => {
      const { guard, bob, wires } = await waitingJoiner([lowId, bobId])
      await later(bob, 60_000)
      guard.stats({ [lowId]: newer(4) })
      await later(bob, 1000)
      expect(wires).toHaveLength(1)
      // A gap longer than the window starts the count again.
      await later(bob, 6000)
      guard.stats({ [lowId]: newer(4) })
      await later(bob, 4000)
      guard.stats({ [lowId]: newer(4) })
      await later(bob, 1000)
      expect(wires).toHaveLength(1)
      for (let second = 0; second < 6; second++) {
        guard.stats({ [lowId]: newer(4) })
        await later(bob, 1000)
      }
      await vi.waitFor(() => expect(wires).toHaveLength(2))
      bob.stop()
    }, 30_000)

    it('believes the committer alone', async () => {
      const { guard, bob, wires } = await waitingJoiner([lowId, bobId])
      for (let second = 0; second < 6; second++) {
        guard.stats({ [lowId]: newer(4) })
        await later(bob, 1000)
      }
      await vi.waitFor(() => expect(wires).toHaveLength(2))
      bob.stop()
    }, 30_000)

    it('backs off when it keeps happening and starts over once frames open again', async () => {
      const { guard, bob, wires } = await waitingJoiner([lowId, bobId])
      const at: number[] = []
      let seconds = 0
      const run = async (count: number, stats: Record<string, FrameStats>) => {
        for (let i = 0; i < count; i++) {
          const before = wires.length
          guard.stats(stats)
          await later(bob, 1000)
          seconds++
          if (wires.length > before) at.push(seconds)
        }
      }
      await run(90, { [lowId]: newer(4) })
      // Five seconds of evidence, then ten, twenty and forty seconds apart.
      expect(at).toHaveLength(4)
      expect(at[0]).toBeLessThanOrEqual(6)
      expect([at[1]! - at[0]!, at[2]! - at[1]!, at[3]! - at[2]!]).toEqual([10, 20, 40])
      // Frames open again: the wait is the first one again.
      guard.stats({ [lowId]: opens })
      await later(bob, 1000)
      const count = wires.length
      await run(7, { [lowId]: newer(7) })
      expect(wires.length).toBe(count + 1)
      bob.stop()
    }, 60_000)

    it('does nothing after the call ended or for an epoch it already holds', async () => {
      const guard = recordingGuard()
      const alice = browserMediaCall(guard)
      alice.start(channelId, aliceId)
      alice.roster([aliceId])
      await mediaSettled(alice)
      expect(guard.keys.map(item => item.epoch)).toEqual([0])
      vi.useFakeTimers({ toFake: ['Date'], now: Date.now() })
      const recover = vi.spyOn(MediaGroup.prototype, 'recover')
      for (let second = 0; second < 8; second++) {
        guard.stats({ [bobId]: newer(0), [carolId]: newer(0) })
        vi.setSystemTime(Date.now() + 1000)
      }
      expect(recover).not.toHaveBeenCalled()
      alice.stop()
      guard.stats({ [bobId]: newer(5), [carolId]: newer(5) })
      expect(recover).not.toHaveBeenCalled()
      recover.mockRestore()
    }, 30_000)
  })

  it('passes on what the group says about this member', async () => {
    let listener: ((notice: 'removed') => void) | null = null
    const spy = vi.spyOn(MediaGroup.prototype, 'onNotice').mockImplementation((next) => { listener = next })
    const call = browserMediaCall(recordingGuard())
    const seen: string[] = []
    call.onNotice(notice => seen.push(notice))
    call.start(channelId, bobId)
    await mediaSettled(call)
    ;(listener as ((notice: 'removed') => void) | null)?.('removed')
    expect(seen).toEqual(['removed'])
    spy.mockRestore()
    call.stop()
    await mediaSettled(call)
  }, 30_000)

  describe('a step that finishes after the call moved to another channel', () => {
    const otherChannel = fixtureId(8)
    const key = {} as EpochKeys

    afterEach(() => { vi.restoreAllMocks() })

    function held<T>() {
      let release: (value: T) => void = () => {}
      const promise = new Promise<T>((resolve) => { release = resolve })
      return { promise, release: (value: T) => release(value) }
    }

    it('does not send its wires or install its key into the new call', async () => {
      const guard = recordingGuard()
      const call = browserMediaCall(guard)
      const sent: string[] = []
      call.onWire((channel, wire) => { sent.push(`${channel}:${wire}`) })
      const slow = held<{ wires: Uint8Array[], epoch: number | null, key: EpochKeys | null }>()
      const started = vi.fn()
      vi.spyOn(MediaGroup.prototype, 'sync').mockImplementationOnce(() => { started(); return slow.promise })
        .mockImplementation(() => Promise.resolve({ wires: [], epoch: null, key: null }))
      call.start(channelId, aliceId)
      await vi.waitFor(() => expect(started).toHaveBeenCalled())
      call.start(otherChannel, aliceId)
      slow.release({ wires: [new Uint8Array([1, 2, 3])], epoch: 7, key })
      await mediaSettled(call)
      expect(sent).toEqual([])
      expect(guard.keys.map(item => item.epoch)).not.toContain(7)
      call.stop()
      await mediaSettled(call)
    }, 30_000)

    it('does not apply a handshake that was being processed', async () => {
      const guard = recordingGuard()
      const call = browserMediaCall(guard)
      const sent: string[] = []
      call.onWire((_channel, wire) => { sent.push(wire) })
      const slow = held<{ wires: Uint8Array[], epoch: number | null, key: EpochKeys | null }>()
      const started = vi.fn()
      vi.spyOn(MediaGroup.prototype, 'receive').mockImplementationOnce(() => { started(); return slow.promise })
      call.start(channelId, aliceId)
      call.roster([aliceId])
      await mediaSettled(call)
      const own = guard.keys.length
      const wire = btoa(String.fromCharCode(1, 2, 3, 4))
      call.receive(bobId, wire)
      await vi.waitFor(() => expect(started).toHaveBeenCalled())
      call.start(otherChannel, aliceId)
      slow.release({ wires: [new Uint8Array([9, 9, 9])], epoch: 9, key })
      await mediaSettled(call)
      expect(sent).not.toContain(btoa(String.fromCharCode(9, 9, 9)))
      expect(guard.keys.map(item => item.epoch)).not.toContain(9)
      expect(guard.keys.length).toBeLessThanOrEqual(own + 1)
      call.stop()
      await mediaSettled(call)
    }, 30_000)

    it('hands on what a timer in the group produced, unless the call moved on', async () => {
      const guard = recordingGuard()
      const call = browserMediaCall(guard)
      const sent: string[] = []
      call.onWire((_channel, wire) => { sent.push(wire) })
      let listener: ((step: { wires: Uint8Array[], epoch: number | null, key: EpochKeys | null }) => void) | null = null
      vi.spyOn(MediaGroup.prototype, 'onStep').mockImplementation((next) => { listener = next })
      call.start(channelId, aliceId)
      await mediaSettled(call)
      const step = { wires: [new Uint8Array([5, 5, 5])], epoch: null, key: null }
      ;(listener as unknown as (value: typeof step) => void)(step)
      expect(sent).toEqual([btoa(String.fromCharCode(5, 5, 5))])
      call.start(otherChannel, aliceId)
      ;(listener as unknown as (value: typeof step) => void)(step)
      await mediaSettled(call)
      expect(sent).toHaveLength(1)
      call.stop()
      await mediaSettled(call)
    }, 30_000)

    it('does nothing for a recovery whose call ended first', async () => {
      const guard = recordingGuard()
      const call = browserMediaCall(guard)
      call.start(channelId, bobId)
      call.roster([lowIdFor, bobId])
      await mediaSettled(call)
      const recover = vi.spyOn(MediaGroup.prototype, 'recover')
      vi.useFakeTimers({ toFake: ['Date'], now: Date.now() })
      for (let second = 0; second < 6; second++) {
        guard.stats({ [lowIdFor]: { opened: 0, newer: 3, epoch: 5 } })
        vi.setSystemTime(Date.now() + 1000)
      }
      call.stop()
      await mediaSettled(call)
      vi.useRealTimers()
      expect(recover).not.toHaveBeenCalled()
    }, 30_000)

    it('does not apply a recovery that was being worked out', async () => {
      const guard = recordingGuard()
      const call = browserMediaCall(guard)
      const sent: string[] = []
      call.onWire((_channel, wire) => { sent.push(wire) })
      call.start(channelId, bobId)
      call.roster([lowIdFor, bobId])
      await mediaSettled(call)
      sent.length = 0
      const slow = held<{ wires: Uint8Array[], epoch: number | null, key: EpochKeys | null }>()
      const started = vi.fn()
      vi.spyOn(MediaGroup.prototype, 'recover').mockImplementationOnce(() => { started(); return slow.promise })
      vi.useFakeTimers({ toFake: ['Date'], now: Date.now() })
      for (let second = 0; second < 7; second++) {
        guard.stats({ [lowIdFor]: { opened: 0, newer: 3, epoch: 5 } })
        vi.setSystemTime(Date.now() + 1000)
      }
      await vi.waitFor(() => expect(started).toHaveBeenCalled())
      call.start(otherChannel, bobId)
      slow.release({ wires: [new Uint8Array([7, 7, 7])], epoch: null, key: null })
      await mediaSettled(call)
      vi.useRealTimers()
      expect(sent).not.toContain(btoa(String.fromCharCode(7, 7, 7)))
      call.stop()
      await mediaSettled(call)
    }, 30_000)
  })
})
