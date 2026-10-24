import type { ApiOptions } from '../lib/api'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ProfileModerationTab from './ProfileModerationTab.vue'
import UserProfileModal from './UserProfileModal.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { PERMISSION_BITS } from '../lib/permissions'
import { fixtureId, userFixture } from '../test-fixtures.fixture'

const http = vi.hoisted(() => ({ calls: [] as Array<{ url: string; options?: ApiOptions }>, handle: (_url: string, _o?: ApiOptions): unknown => ({}) }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => {
    http.calls.push({ url, ...(options ? { options } : {}) })
    try { return Promise.resolve(http.handle(url, options)) } catch (error) { return Promise.reject(error) }
  })
}))
const confirmMock = vi.hoisted(() => vi.fn())
vi.mock('../lib/confirm', () => ({ confirm: confirmMock }))

const global = { plugins: [i18nPlugin], directives: { tooltip } }
const subject = userFixture({ id: fixtureId(9), username: 'max', display_name: 'Max', role: 'user' })
const note = (n: number, extra: Record<string, unknown> = {}) => ({
  id: fixtureId(n), author_id: fixtureId(1), author_name: 'Mod', body: `note ${n}`, strike: false, created_at: '2026-01-02T03:04:05Z',
  updated_at: '2026-01-02T03:04:05Z', mine: true, can_delete: true, ...extra
})
const event = (kind: string, extra: Record<string, unknown> = {}) => ({ kind, at: '2026-01-02T03:04:05Z', ...extra })
let history: { strikes: number; notes: unknown[]; events: unknown[] }

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  http.calls = []
  confirmMock.mockReset()
  history = { strikes: 1, notes: [note(1, { strike: true }), note(2, { mine: false, can_delete: false, author_name: 'Deleted member' })], events: [
    event('timeout', { actor: 'Hammer', duration_seconds: 600, reason: 'cool off' }),
    event('automod', { rule: 'No spam', outcome: 'blocked', hits: 3 }),
    event('roles', { actor: 'Owner', added_roles: ['Regular'], removed_roles: ['Guest'] }),
    event('report', { category: 'harassment', status: 'open' }),
    event('unban', { actor: 'Hammer' })
  ] }
  http.handle = (url, o) => (o?.method === 'POST' || o?.method === 'PUT' ? note(7) : o?.method === 'DELETE' ? undefined : history)
})
afterEach(() => { document.body.innerHTML = '' })

describe('ProfileModerationTab', () => {
  it('shows the notes and the timeline in words and tells the strike count', async () => {
    const w = mount(ProfileModerationTab, { props: { userId: subject.id }, global })
    await flushPromises()
    expect(http.calls[0]!.url).toBe(`/api/admin/users/${subject.id}/moderation`)
    expect(w.emitted('strikes')).toEqual([[1]])
    expect(w.findAll('[data-note]')).toHaveLength(2)
    expect(w.findAll('[data-testid="mod-note-strike-badge"]')).toHaveLength(1)
    // An erased author reads as in the rest of the app.
    expect(w.findAll('[data-note]')[1]!.text()).toContain('Deleted member')
    const lines = w.findAll('[data-kind]').map(l => l.text())
    expect(lines[0]).toContain('Hammer timed them out for 10 min')
    expect(lines[0]).toContain('Reason: cool off')
    expect(lines[1]).toContain('Automod rule “No spam” blocked a message ×3')
    expect(lines[2]).toContain('Owner changed their roles: +Regular, −Guest')
    expect(lines[3]).toContain('Reported (Harassment), Open')
    expect(lines[4]).toContain('Hammer lifted the ban')
    expect(w.text()).toContain('never shown it')
  })

  it('adds a note, as a strike if asked, and reloads', async () => {
    const w = mount(ProfileModerationTab, { props: { userId: subject.id }, global })
    await flushPromises()
    expect(w.get('[data-testid="mod-note-add"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="mod-note-input"]').setValue('  third warning  ')
    await w.get('[data-testid="mod-note-strike"]').setValue(true)
    history = { ...history, strikes: 2 }
    await w.get('form').trigger('submit')
    await flushPromises()
    const post = http.calls.find(c => c.options?.method === 'POST')
    expect(post?.url).toBe(`/api/admin/users/${subject.id}/moderation/notes`)
    expect(post?.options?.json).toEqual({ body: 'third warning', strike: true })
    expect(w.emitted('strikes')?.at(-1)).toEqual([2])
    expect((w.get('[data-testid="mod-note-input"]').element as HTMLTextAreaElement).value).toBe('')
  })

  it('lets the author edit and delete their note and nobody else', async () => {
    const w = mount(ProfileModerationTab, { props: { userId: subject.id }, global })
    await flushPromises()
    const rows = w.findAll('[data-note]')
    expect(rows[0]!.find('[data-testid="mod-note-edit"]').exists()).toBe(true)
    expect(rows[1]!.find('[data-testid="mod-note-edit"]').exists()).toBe(false)
    expect(rows[1]!.find('[data-testid="mod-note-delete"]').exists()).toBe(false)

    await rows[0]!.get('[data-testid="mod-note-edit"]').trigger('click')
    await w.get('[data-testid="mod-note-edit-input"]').setValue('note 1, updated')
    await w.get('[data-testid="mod-note-save"]').trigger('click')
    await flushPromises()
    const put = http.calls.find(c => c.options?.method === 'PUT')
    expect(put?.url).toBe(`/api/admin/users/${subject.id}/moderation/notes/${fixtureId(1)}`)
    expect(put?.options?.json).toEqual({ body: 'note 1, updated', strike: true })

    confirmMock.mockResolvedValueOnce(false)
    await w.get('[data-testid="mod-note-delete"]').trigger('click')
    await flushPromises()
    expect(http.calls.some(c => c.options?.method === 'DELETE')).toBe(false)
    confirmMock.mockResolvedValueOnce(true)
    await w.get('[data-testid="mod-note-delete"]').trigger('click')
    await flushPromises()
    expect(http.calls.some(c => c.options?.method === 'DELETE' && c.url.endsWith(`/notes/${fixtureId(1)}`))).toBe(true)
  })

  it('says so when nothing is on record', async () => {
    history = { strikes: 0, notes: [], events: [] }
    const w = mount(ProfileModerationTab, { props: { userId: subject.id }, global })
    await flushPromises()
    expect(w.find('[data-testid="mod-notes-empty"]').exists()).toBe(true)
    expect(w.find('[data-testid="mod-events-empty"]').exists()).toBe(true)
  })

  it('shows a refusal instead of an empty tab', async () => {
    http.handle = () => { throw new Error('nope') }
    const w = mount(ProfileModerationTab, { props: { userId: subject.id }, global })
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('Couldn’t load the moderation history.')
    expect(useToastStore().toasts).toHaveLength(1)
  })
})

describe('ProfileModerationTab details', () => {
  async function load(events: unknown[], notes: unknown[] = []) {
    history = { strikes: 0, notes, events }
    const w = mount(ProfileModerationTab, { props: { userId: subject.id }, global })
    await flushPromises()
    return w
  }

  it('words every kind of event, with fallbacks for missing details', async () => {
    const w = await load([
      event('timeout'),
      event('ban', { actor: 'Hammer' }),
      event('ban', { actor: 'Hammer', duration_seconds: 3600 }),
      event('roles', { added_roles: ['Regular'] }),
      event('roles', { removed_roles: ['Guest'] }),
      event('report'),
      event('automod', { rule: 'No spam' }),
      event('automod', { rule: 'No spam', outcome: 'blocked', hits: 1 })
    ])
    const lines = w.findAll('[data-kind]').map(l => l.text())
    expect(lines[0]).toMatch(/timed them out/)
    expect(lines[0]).toContain('A moderator')
    expect(lines[1]).toContain('Hammer')
    expect(lines[2]).toContain('Hammer')
    expect(lines[2]).not.toBe(lines[1])
    expect(lines[3]).toContain('+Regular')
    expect(lines[4]).toContain('−Guest')
    expect(lines[5]).toContain('Reported')
    expect(lines[6]).toContain('No spam')
    expect(lines[6]).not.toContain('×')
    expect(lines[7]).not.toContain('×')
  })

  it('does not add a blank note', async () => {
    const w = await load([])
    await w.get('[data-testid="mod-note-input"]').setValue('   ')
    await w.get('form').trigger('submit')
    expect(http.calls.some(c => c.options?.method === 'POST')).toBe(false)
  })

  it('keeps the draft and toasts when saving a note fails', async () => {
    const w = await load([])
    http.handle = (_url, o) => { if (o?.method === 'POST') throw new Error('save broke'); return history }
    await w.get('[data-testid="mod-note-input"]').setValue('keep me')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect((w.get('[data-testid="mod-note-input"]').element as HTMLTextAreaElement).value).toBe('keep me')
    expect(useToastStore().toasts.at(-1)?.text).toBe('save broke')
  })

  it('cancels an edit and refuses an empty edit', async () => {
    const w = await load([], [note(1)])
    await w.get('[data-testid="mod-note-edit"]').trigger('click')
    expect((w.get('[data-testid="mod-note-edit-input"]').element as HTMLTextAreaElement).value).toBe('note 1')
    await w.get('[data-testid="mod-note-edit-input"]').setValue('  ')
    await w.get('[data-testid="mod-note-save"]').trigger('click')
    expect(http.calls.some(c => c.options?.method === 'PUT')).toBe(false)
    const cancel = w.findAll('[data-note] button').find(b => b.text() === 'Cancel')!
    await cancel.trigger('click')
    expect(w.find('[data-testid="mod-note-edit-input"]').exists()).toBe(false)
    expect(w.get('[data-note]').text()).toContain('note 1')
  })

  it('toasts when deleting a note fails and leaves the note in place', async () => {
    const w = await load([], [note(1)])
    http.handle = (_url, o) => { if (o?.method === 'DELETE') throw new Error('delete broke'); return history }
    confirmMock.mockResolvedValueOnce(true)
    await w.get('[data-testid="mod-note-delete"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.text).toBe('delete broke')
    expect(w.findAll('[data-note]')).toHaveLength(1)
  })

  it('keeps the loaded history when a refresh after a change fails', async () => {
    const w = await load([], [note(1)])
    let fail = false
    http.handle = (_url, o) => { if (fail && !o?.method) throw new Error('reload broke'); return o?.method === 'DELETE' ? undefined : history }
    confirmMock.mockResolvedValueOnce(true)
    fail = true
    await w.get('[data-testid="mod-note-delete"]').trigger('click')
    await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true)
  })
})

describe('UserProfileModal moderation tab', () => {
  function open(bits: number, features: string[], viewer = fixtureId(1)) {
    useAuthStore().user = userFixture({ id: viewer, username: 'mod', role: 'user' })
    usePermissionsStore().info = { owner: false, permissions: bits, max_upload_mb: 50, role_ids: [], top_position: 5, catalog: [], channels: [] }
    useServerFeaturesStore().list = features
    useChatStore().selectedUserProfile = { ...subject }
    return mount(UserProfileModal, { global, attachTo: document.body })
  }

  it('is there for Manage reports, with the strike badge, and the profile stays one tap away', async () => {
    const w = open(PERMISSION_BITS.manage_reports, ['moderation.history'])
    await flushPromises()
    expect(w.find('[data-testid="profile-tabs"]').exists()).toBe(true)
    expect(w.get('[data-testid="strike-badge"]').text()).toBe('1')
    const tab = w.get('[data-testid="profile-moderation-tab"]')
    expect(tab.isVisible()).toBe(false)
    await w.get('[data-tab="moderation"]').trigger('click')
    expect(tab.isVisible()).toBe(true)
    await w.get('[data-tab="profile"]').trigger('click')
    expect(tab.isVisible()).toBe(false)
    w.unmount()
  })

  it('is absent without the permission, on an older server, for yourself and for bots', async () => {
    for (const w of [open(0, ['moderation.history']), open(PERMISSION_BITS.manage_reports, []), open(PERMISSION_BITS.timeout_members | PERMISSION_BITS.ban_members, ['moderation.history'])]) {
      await flushPromises()
      expect(w.find('[data-testid="profile-tabs"]').exists()).toBe(false)
      expect(w.find('[data-testid="profile-moderation-tab"]').exists()).toBe(false)
      w.unmount()
    }
    http.calls = []
    const own = open(PERMISSION_BITS.manage_reports, ['moderation.history'], subject.id)
    await flushPromises()
    expect(own.find('[data-testid="profile-tabs"]').exists()).toBe(false)
    expect(http.calls.some(c => c.url.includes('/moderation'))).toBe(false)
    own.unmount()
    useChatStore().members = [{ ...subject, is_bot: true } as never]
    const bot = open(PERMISSION_BITS.manage_reports, ['moderation.history'])
    await flushPromises()
    expect(bot.find('[data-testid="profile-tabs"]').exists()).toBe(false)
    bot.unmount()
  })
})
