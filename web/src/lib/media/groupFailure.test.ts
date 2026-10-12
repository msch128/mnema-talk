import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { fixtureId } from '../../test-fixtures.fixture'
import { HANDSHAKE_MAX_BYTES } from './bounds'
import { openFrame, sealFrame } from './frame'
import { MediaGroup } from './group'

const faults = vi.hoisted(() => ({
  createGroup: false,
  createCommit: false,
  joinGroup: false,
  encode: '' as '' | 'throw' | 'huge',
  epochKey: false,
  hugeEpoch: false,
  privateCommit: false,
  blankTag: false,
}))

vi.mock('ts-mls', async importOriginal => {
  const actual = await importOriginal<typeof import('ts-mls')>()
  return {
    ...actual,
    createGroup: async (...args: Parameters<typeof actual.createGroup>) => {
      if (faults.createGroup) return Promise.reject(new Error('group failed'))
      const state = await actual.createGroup(...args)
      if (faults.hugeEpoch) state.groupContext.epoch = 0x1_0000_0000n
      if (faults.blankTag) Object.assign(state, { confirmationTag: undefined })
      return state
    },
    createCommit: async (...args: Parameters<typeof actual.createCommit>) => {
      if (faults.createCommit) return Promise.reject(new Error('commit failed'))
      const result = await actual.createCommit(...args)
      if (!faults.privateCommit) return result
      return { ...result, commit: { ...result.commit, wireformat: 'mls_welcome' } as typeof result.commit }
    },
    joinGroup: (...args: Parameters<typeof actual.joinGroup>) => {
      if (faults.joinGroup) return Promise.reject(new Error('welcome rejected'))
      return actual.joinGroup(...args)
    },
    encodeMlsMessage: (...args: Parameters<typeof actual.encodeMlsMessage>) => {
      if (faults.encode === 'throw') throw new Error('encode failed')
      if (faults.encode === 'huge') return new Uint8Array(HANDSHAKE_MAX_BYTES + 1)
      return actual.encodeMlsMessage(...args)
    },
  }
})

vi.mock('./frame', async importOriginal => {
  const actual = await importOriginal<typeof import('./frame')>()
  return {
    ...actual,
    importEpochKey: (raw: Uint8Array) => faults.epochKey ? Promise.resolve(null) : actual.importEpochKey(raw),
  }
})

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const bobId = fixtureId(2)
const channelId = fixtureId(9)

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

async function opened(userId: string) {
  const group = await MediaGroup.open(userId, channelId)
  if (!group) throw new Error('expected a media group')
  return group
}

function wireAt(wires: Uint8Array[], index: number): Uint8Array {
  const wire = wires[index]
  if (!wire) throw new Error('expected a handshake wire')
  return wire
}

async function roundTrip(left: CryptoKey | null, right: CryptoKey | null, epoch: number) {
  if (!left || !right) throw new Error('expected epoch keys')
  const packet = await sealFrame({ key: left, epoch, channelId, senderId: aliceId, plaintext: new Uint8Array([9, 8, 7]) })
  if (!packet) throw new Error('expected a sealed frame')
  const openedFrame = await openFrame({ keys: new Map([[epoch, right]]), channelId, senderId: aliceId, packet })
  return openedFrame ? [...openedFrame] : null
}

describe('media group failures', () => {
  it('keeps the previous group when publishing or joining fails', async () => {
    const alice = await opened(aliceId)
    faults.createGroup = true
    await expect(alice.sync([aliceId])).rejects.toThrow('group failed')
    faults.createGroup = false
    expect((await alice.sync([aliceId])).epoch).toBe(0)

    const bob = await opened(bobId)
    faults.epochKey = true
    expect((await bob.sync([bobId])).key).toBeNull()
    faults.epochKey = false
    expect((await bob.sync([bobId])).epoch).toBe(0)

    faults.encode = 'throw'
    expect((await bob.sync([aliceId, bobId])).wires).toEqual([])
    faults.encode = ''
    const offer = await bob.sync([aliceId, bobId])
    expect(offer.wires.length).toBeGreaterThan(0)

    faults.createCommit = true
    expect((await alice.receive(bobId, wireAt(offer.wires, 0))).wires).toEqual([])
    faults.createCommit = false
    faults.privateCommit = true
    expect((await alice.receive(bobId, wireAt(offer.wires, 0))).wires).toEqual([])
    faults.privateCommit = false
    faults.epochKey = true
    expect((await alice.receive(bobId, wireAt(offer.wires, 0))).epoch).toBe(0)
    faults.epochKey = false
    faults.encode = 'huge'
    const blocked = await alice.receive(bobId, wireAt(offer.wires, 0))
    faults.encode = ''
    expect(blocked.wires).toEqual([])
    expect(blocked.epoch).toBe(0)
    const sealed = await alice.receive(bobId, wireAt(offer.wires, 0))
    expect(sealed.epoch).toBe(1)

    faults.joinGroup = true
    expect((await bob.receive(aliceId, wireAt(sealed.wires, 0))).key).toBeNull()
    faults.joinGroup = false
    const joined = await bob.receive(aliceId, wireAt(sealed.wires, 0))
    expect(joined.epoch).toBe(1)
    expect(await roundTrip(sealed.key, joined.key, 1)).toEqual([9, 8, 7])
    await alice.close()
    await bob.close()

    const carol = await opened(fixtureId(3))
    faults.hugeEpoch = true
    expect((await carol.sync([fixtureId(3)])).key).toBeNull()
    faults.hugeEpoch = false
    faults.blankTag = true
    expect((await carol.sync([fixtureId(3)])).epoch).toBe(0)
    faults.blankTag = false
    await carol.close()
  }, 30_000)
})
