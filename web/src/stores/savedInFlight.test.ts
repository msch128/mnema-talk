import { describe, expect, it } from 'vitest'
import { ref } from 'vue'
import type { ChatSavedMessage } from '../types/rest'
import { createInFlightChanges } from './savedInFlight'
import { fixtureId, FIXTURE_TIMESTAMP, messageFixture } from '../test-fixtures.fixture'

const entry = (id: number, parent?: number): ChatSavedMessage => {
  const e: ChatSavedMessage = { message_id: fixtureId(id), note: '', saved_at: FIXTURE_TIMESTAMP, available: true }
  if (parent !== undefined) e.message = messageFixture({ id: fixtureId(id), parent_id: fixtureId(parent) })
  return e
}

/** A page whose answer is held until land() and then shown, as savedList.load does. */
function setup() {
  const items = ref<ChatSavedMessage[]>([])
  const known = ref(new Set<string>())
  const changes = createInFlightChanges(items, known)
  let land: (page: ChatSavedMessage[]) => void = () => undefined
  const read = () => new Promise<boolean>(resolve => {
    land = page => {
      items.value = page
      known.value = new Set(page.map(e => e.message_id))
      resolve(true)
    }
  })
  return { items, known, changes, read, land: (page: ChatSavedMessage[]) => land(page) }
}

describe('changes made while a saved page is on its way', () => {
  it('are applied to the page again: a removal, a save, and the replies of a deleted root', async () => {
    const s = setup()
    const page = s.changes.tracked(s.read, true)
    expect(s.changes.inFlight()).toBe(true)
    s.changes.remember(fixtureId(1), null)
    s.changes.remember(fixtureId(2), entry(2))
    s.changes.rememberDeletedRoot(fixtureId(5))
    s.land([entry(1), entry(3), entry(6, 5)])
    expect(await page).toBe(true)
    expect(s.items.value.map(e => e.message_id)).toEqual([fixtureId(2), fixtureId(3)])
    expect(s.known.value.has(fixtureId(1))).toBe(false)
    expect(s.known.value.has(fixtureId(6))).toBe(false)
    expect(s.changes.inFlight()).toBe(false)
  })

  it('an appended older page brings back no removal and puts no save on top', async () => {
    const s = setup()
    const page = s.changes.tracked(s.read, false)
    s.changes.remember(fixtureId(1), null)
    s.changes.remember(fixtureId(2), entry(2))
    s.land([entry(3), entry(1)])
    await page
    expect(s.items.value.map(e => e.message_id)).toEqual([fixtureId(3)])
  })

  it('are not kept once no page is on its way, nor when they came before the page was asked for', async () => {
    const s = setup()
    s.changes.remember(fixtureId(1), null) // nothing on its way: nothing to re-apply later
    const first = s.changes.tracked(s.read, true)
    s.land([entry(1)])
    await first
    expect(s.items.value.map(e => e.message_id)).toEqual([fixtureId(1)])
  })
})
