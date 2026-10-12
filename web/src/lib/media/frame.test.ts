import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { importEpochKey, openFrame, rememberEpochKey, sealFrame } from './frame'

const previous = globalThis.crypto

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

async function keyFrom(byte: number) {
  const raw = new Uint8Array(16).fill(byte)
  const key = await importEpochKey(raw)
  if (!key) throw new Error('expected an epoch key')
  return key
}

describe('media frames', () => {
  it('opens a sealed frame only for the same channel, sender and key', async () => {
    const key = await keyFrom(7)
    const packet = await sealFrame({ key, epoch: 4, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([1, 2, 3]) })
    if (!packet) throw new Error('expected a sealed frame')
    expect(packet[0]).toBe(0x4d)
    expect(packet[1]).toBe(0x31)
    const keys = new Map([[4, key]])
    await expect(openFrame({ keys, channelId: 'channel', senderId: 'alice', packet })).resolves.toEqual(new Uint8Array([1, 2, 3]))
    await expect(openFrame({ keys, channelId: 'other', senderId: 'alice', packet })).resolves.toBeNull()
    await expect(openFrame({ keys, channelId: 'channel', senderId: 'bob', packet })).resolves.toBeNull()
    await expect(openFrame({ keys: new Map([[4, await keyFrom(8)]]), channelId: 'channel', senderId: 'alice', packet })).resolves.toBeNull()
    await expect(openFrame({ keys: new Map(), channelId: 'channel', senderId: 'alice', packet })).resolves.toBeNull()
  })

  it('rejects a short, unmarked, shifted or unusable frame', async () => {
    const key = await keyFrom(1)
    const packet = await sealFrame({ key, epoch: 1, channelId: 'c', senderId: 'a', plaintext: new Uint8Array() })
    if (!packet) throw new Error('expected a sealed frame')
    expect(await openFrame({ keys: new Map([[1, key]]), channelId: 'c', senderId: 'a', packet: packet.slice(0, 8) })).toBeNull()
    const unmarked = packet.slice()
    unmarked[0] = 0
    expect(await openFrame({ keys: new Map([[1, key]]), channelId: 'c', senderId: 'a', packet: unmarked })).toBeNull()
    const shifted = packet.slice()
    const last = shifted.length - 1
    const current = shifted[last]
    if (current === undefined) throw new Error('expected a sealed frame')
    shifted[last] = current ^ 0xff
    expect(await openFrame({ keys: new Map([[1, key]]), channelId: 'c', senderId: 'a', packet: shifted })).toBeNull()
    expect(await sealFrame({ key, epoch: -1, channelId: 'c', senderId: 'a', plaintext: new Uint8Array() })).toBeNull()
    expect(await sealFrame({ key, epoch: 1.5, channelId: 'c', senderId: 'a', plaintext: new Uint8Array() })).toBeNull()
    expect(await sealFrame({ key, epoch: 0x1_0000_0000, channelId: 'c', senderId: 'a', plaintext: new Uint8Array() })).toBeNull()
    expect(await importEpochKey(new Uint8Array(15))).toBeNull()
    const refused = vi.spyOn(crypto.subtle, 'importKey').mockRejectedValueOnce(new Error('refused'))
    await expect(importEpochKey(new Uint8Array(16))).resolves.toBeNull()
    refused.mockRestore()
    const decryptOnly = await crypto.subtle.importKey('raw', new Uint8Array(16), 'AES-GCM', false, ['decrypt'])
    expect(await sealFrame({ key: decryptOnly, epoch: 1, channelId: 'c', senderId: 'a', plaintext: new Uint8Array([1]) })).toBeNull()
  })

  it('keeps the two newest epoch keys', () => {
    const keys = new Map<number, CryptoKey>()
    const first = {} as CryptoKey
    const second = {} as CryptoKey
    const third = {} as CryptoKey
    rememberEpochKey(keys, 1, first)
    rememberEpochKey(keys, 3, third)
    rememberEpochKey(keys, 2, second)
    expect([...keys.keys()].sort((a, b) => a - b)).toEqual([2, 3])
    expect(keys.get(2)).toBe(second)
    expect(keys.get(3)).toBe(third)
  })
})
