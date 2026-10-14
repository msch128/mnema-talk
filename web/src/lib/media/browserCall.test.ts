import { webcrypto } from 'node:crypto'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { fixtureId } from '../../test-fixtures.fixture'
import { HANDSHAKE_MAX_BYTES } from './bounds'
import type { MediaGuard } from './guard'
import { browserMediaCall, mediaSettled } from './browserCall'
import { MediaGroup } from './group'
import { openFrame, sealFrame } from './frame'
import type { MediaCall } from './api'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const channelId = fixtureId(9)

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

function recordingGuard(): MediaGuard & { keys: { epoch: number, key: CryptoKey, replace: boolean }[], sends: string[], receives: string[], miss(epoch: number): void } {
  const keys: { epoch: number, key: CryptoKey, replace: boolean }[] = []
  const sends: string[] = []
  const receives: string[] = []
  let missListener: (epoch: number) => void = () => {}
  return {
    miss: epoch => missListener(epoch),
    onEpochMiss(listener) { missListener = listener },
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
    const packet = await sealFrame({
      key: aliceKey.key, epoch: aliceKey.epoch, channelId, senderId: aliceId, plaintext: new Uint8Array([1, 2]),
    })
    if (!packet) throw new Error('expected a sealed frame')
    await expect(openFrame({
      keys: new Map([[bobKey.epoch, bobKey.key]]), channelId, senderId: aliceId, packet,
    })).resolves.toEqual(new Uint8Array([1, 2]))
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
    const replacement = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 128 }, false, ['encrypt', 'decrypt'])
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

  describe('frames of an epoch that never arrives', () => {
    afterEach(() => { vi.useRealTimers() })

    async function waitingJoiner() {
      const guard = recordingGuard()
      const bob = browserMediaCall(guard)
      const wires: string[] = []
      bob.onWire((_channel, wire) => { wires.push(wire) })
      bob.start(channelId, bobId)
      bob.roster([aliceId, bobId])
      await mediaSettled(bob)
      expect(wires).toHaveLength(1)
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] })
      return { guard, bob, wires }
    }

    it('asks again with a fresh key package once the epoch stays missing', async () => {
      const { guard, bob, wires } = await waitingJoiner()
      guard.miss(3)
      guard.miss(2)
      await vi.advanceTimersByTimeAsync(3999)
      expect(wires).toHaveLength(1)
      await vi.advanceTimersByTimeAsync(2)
      await vi.waitFor(() => expect(wires).toHaveLength(2))
      expect(wires[1]).not.toBe(wires[0])
      // Not again right away.
      guard.miss(3)
      await vi.advanceTimersByTimeAsync(5000)
      await mediaSettled(bob)
      expect(wires).toHaveLength(2)
      await vi.advanceTimersByTimeAsync(2000)
      guard.miss(3)
      await vi.advanceTimersByTimeAsync(11_000)
      await vi.waitFor(() => expect(wires).toHaveLength(3))
      bob.stop()
    }, 30_000)

    it('does not start over when the key arrives while it waits', async () => {
      const guard = recordingGuard()
      const alice = browserMediaCall(guard)
      const bob = browserMediaCall(recordingGuard())
      alice.onWire((_channel, wire) => { bob.receive(aliceId, wire) })
      bob.onWire((_channel, wire) => { alice.receive(bobId, wire) })
      alice.start(channelId, aliceId)
      alice.roster([aliceId])
      await mediaSettled(alice)
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] })
      const recover = vi.spyOn(MediaGroup.prototype, 'recover')
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] })
      guard.miss(1)
      bob.start(channelId, bobId)
      alice.roster([aliceId, bobId])
      bob.roster([aliceId, bobId])
      for (let turn = 0; turn < 8; turn++) {
        await mediaSettled(alice)
        await mediaSettled(bob)
      }
      expect(guard.keys.at(-1)?.epoch).toBe(1)
      await vi.advanceTimersByTimeAsync(5000)
      expect(recover).not.toHaveBeenCalled()
      recover.mockRestore()
      alice.stop()
      bob.stop()
    }, 30_000)

    it('does nothing for an epoch it already has, after stop, or when the key shows up', async () => {
      const { guard, bob, wires } = await waitingJoiner()
      guard.miss(3)
      bob.stop()
      await vi.advanceTimersByTimeAsync(5000)
      expect(wires).toHaveLength(1)
      const alice = browserMediaCall(guard)
      alice.start(channelId, aliceId)
      alice.roster([aliceId])
      await mediaSettled(alice)
      expect(guard.keys.map(item => item.epoch)).toEqual([0])
      guard.miss(0)
      await vi.advanceTimersByTimeAsync(5000)
      guard.miss(5)
      alice.stop()
      await vi.advanceTimersByTimeAsync(5000)
      await mediaSettled(alice)
    }, 30_000)
  })
})
