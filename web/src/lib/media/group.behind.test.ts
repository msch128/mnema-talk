// A member that reloads makes a second key package. If the relay hands it to the committer only, the committer
// swaps the member's leaf and every other member refuses the commit ("an add of a package I never saw"): they
// are one epoch behind and nothing tells them. They must find out within an offer wait, not at the next
// rotation or after two heartbeats of the lowest id.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { MediaGroup } from './group'
import { ROOM_CHANNEL, ROOM_IDS, TalkRoom } from './groupRoom.fixture'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

const [first, second, third, fourth] = ROOM_IDS as [string, string, string, string]
const roster = [first, second, third, fourth]

/** Four members in one group; the third reloads, and its new tab's package reaches only the committer. */
async function reloaded() {
  const room = new TalkRoom(false)
  await room.form(roster)
  expect(room.agreed()).toBe(true)
  await room.hang(third)
  const tab = await MediaGroup.open(third, ROOM_CHANNEL) as MediaGroup
  room.groups.set(third, tab)
  // The new tab is in another part until the package has been placed: what it says first reaches nobody.
  room.part.set(third, 1)
  let offer: Uint8Array | null = null
  room.onWire = (from, kind, wire) => { if (from === third && kind === 'key-package') offer = wire }
  await room.list(roster)
  room.part.set(third, 0)
  room.onWire = () => {}
  if (!offer) throw new Error('expected the new tab to offer a key package')
  return { room, offer: offer as Uint8Array }
}

describe('a commit that adds a key package this member never saw', () => {
  it('leaves nobody behind: the members that did not get the package start over and the room agrees', async () => {
    const { room, offer } = await reloaded()
    await room.inject(third, offer, [first])
    expect(room.agreed()).toBe(false)
    expect(await room.run(60_000, () => room.agreed())).toBe(true)
    await room.close()
  }, 120_000)

  it('does not start over when the package arrives within the wait: the commit is taken as it stands', async () => {
    const { room, offer } = await reloaded()
    await room.inject(third, offer, [first])
    room.sent.clear()
    await room.run(1000)
    await room.inject(third, offer, [second, fourth])
    expect(await room.run(10_000, () => room.agreed())).toBe(true)
    expect(room.count('key-package')).toBe(0)
    await room.close()
  }, 120_000)
})
