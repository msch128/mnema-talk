import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useEventCommentsStore } from './eventComments'
import { useToastStore } from './toast'
import { setLocale } from '../i18n'
import { fixtureId } from '../test-fixtures.fixture'

const openEvents = vi.fn()
vi.mock('../lib/eventsDialog', () => ({ openEvents: (id: string) => openEvents(id) }))

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  openEvents.mockReset()
})

describe('event comments store', () => {
  it('numbers the live changes so a repeated one is still seen', () => {
    const store = useEventCommentsStore()
    const change = { action: 'deleted' as const, event_id: fixtureId(1), comment_id: fixtureId(2) }
    store.handleChange(change)
    store.handleChange(change)
    expect(store.last?.seq).toBe(2)
    store.reset()
    expect(store.last).toBeNull()
  })

  it('tells about a new comment calmly and opens the event from the toast', () => {
    useEventCommentsStore().handleNotice({ event_id: fixtureId(1), title: 'Raid', author_name: 'Ada', excerpt: 'See you' })
    const toast = useToastStore().toasts[0]!
    expect(toast.type).toBe('info')
    expect(toast.text).toBe('New comment on Raid: Ada: See you')
    toast.action?.onClick()
    expect(openEvents).toHaveBeenCalledWith(fixtureId(1))
  })
})
