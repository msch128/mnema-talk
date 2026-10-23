import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { usePermissionsStore } from './permissions'
import { useToastStore } from './toast'
import { fixtureId, userFixture, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.spyOn(window, 'addEventListener'); vi.spyOn(document, 'addEventListener')
})
afterEach(() => {
  useChatStore().closeWebSocket()
  for (const [type, listener, options] of vi.mocked(window.addEventListener).mock.calls) window.removeEventListener(type, listener, options)
  for (const [type, listener, options] of vi.mocked(document.addEventListener).mock.calls) document.removeEventListener(type, listener, options)
  vi.restoreAllMocks()
})

describe('moderation events', () => {
  it('marks a member as timed out and clears it again', () => {
    const chat = useChatStore()
    const member = userFixture({ id: fixtureId(9) })
    chat.members = [member]
    chat.handleWSEvent({ type: 'member_timeout', payload: { id: member.id, timeout_until: FIXTURE_TIMESTAMP } })
    expect(chat.members[0]?.timeout_until).toBe(FIXTURE_TIMESTAMP)
    chat.handleWSEvent({ type: 'member_timeout', payload: { id: member.id, timeout_until: null } })
    expect(chat.members[0] && 'timeout_until' in chat.members[0]).toBe(false)
    // A member the list does not hold is ignored.
    chat.handleWSEvent({ type: 'member_timeout', payload: { id: fixtureId(8), timeout_until: FIXTURE_TIMESTAMP } })
    expect(chat.members).toHaveLength(1)
  })

  it('tells the member what happened to them and refetches the permissions', () => {
    const chat = useChatStore()
    const load = vi.spyOn(usePermissionsStore(), 'load').mockResolvedValue()
    chat.handleWSEvent({ type: 'moderation_notice', payload: { kind: 'timeout', duration_seconds: 3600, reason: 'spam' } })
    expect(useToastStore().toasts.map(t => t.text)).toContain('You were timed out for 1 h: spam')
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('shows a calm note when a moderator kicked the member', () => {
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'kicked', payload: { reason: 'take a break' } })
    const [note] = useToastStore().toasts
    expect(note?.text).toContain('signed out by a moderator')
    expect(note?.text).toContain('take a break')
    expect(note?.type).toBe('info')
  })
})
