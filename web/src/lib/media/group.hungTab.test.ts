// A tab that hangs stays in the Talk's list. The others remove it from the
// group after the absence limit and must then go on undisturbed, on production
// timing, for as long as it stays listed.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { ROOM_IDS, TalkRoom } from './groupRoom.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

const HOUR = 60 * 60_000

describe('the lowest tab that hangs in a room it founded', () => {
  it.each([3, 4])('is not let in again from the key package it announced before the others joined, room of %i', async (size) => {
    const room = new TalkRoom()
    const everyone = ROOM_IDS.slice(0, size)
    // It founded alone and said so when the second member arrived: that one kept the package and joined its group.
    await room.form(everyone)
    expect(room.agreed()).toBe(true)
    await room.hang(everyone[0] as string)
    room.sent.clear()
    expect(await room.secondsApart(HOUR)).toBe(0)
    expect(room.count('welcome')).toBe(0)
    expect(room.count('key-package')).toBe(0)
    await room.close()
  }, 300_000)
})
