import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminAutomodTab from './AdminAutomodTab.vue'
import AdminDashboard from './AdminDashboard.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { PERMISSION_BITS } from '../lib/permissions'
import { ApiError } from '../lib/api'
import type { ApiOptions } from '../lib/api'

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
const rule = (n: number, extra: Record<string, unknown> = {}) => ({
  id: fixtureId(n), kind: 'mentions', name: 'Mention spam', enabled: true, action: 'block', timeout_seconds: 0,
  config: { max_mentions: 10 }, exempt_roles: [], exempt_channels: [], updated_at: '2026-01-01T00:00:00Z', ...extra
})
const raid = rule(2, { kind: 'raid', name: 'Raid protection', action: 'flag', config: { joins: 10, window_seconds: 10, restrict_seconds: 600 } })
let rules: unknown[]

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  http.calls = []
  rules = [rule(1), raid]
  http.handle = (url, o) => {
    if (url === '/api/admin/automod') return { rules }
    return o?.json ? { ...(o.json as object), id: fixtureId(9), updated_at: '2026-01-01T00:00:00Z', exempt_roles: [], exempt_channels: [] } : undefined
  }
  confirmMock.mockReset()
})
afterEach(() => { document.body.innerHTML = '' })

describe('AdminAutomodTab', () => {
  it('lists the rules with what they do', async () => {
    const w = mount(AdminAutomodTab, { global })
    await flushPromises()
    const rows = w.findAll('[data-rule]')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.text()).toContain('Mention spam')
    expect(rows[0]!.get('[data-testid="rule-summary"]').text()).toBe('More than 10 different mentions')
    expect(rows[1]!.text()).toContain('More than 10 joins within 10 s')
    // Raid protection only flags: no action badge.
    expect(rows[1]!.text()).not.toContain('Blocks')
  })

  it('offers raid protection only while it does not exist', async () => {
    const w = mount(AdminAutomodTab, { global })
    await flushPromises()
    expect(w.findAll('[data-add]').map(b => b.attributes('data-add'))).toEqual(['words', 'links', 'mentions', 'new_account'])
    rules = [rule(1)]
    const again = mount(AdminAutomodTab, { global })
    await flushPromises()
    expect(again.find('[data-add="raid"]').exists()).toBe(true)
  })

  it('switches a rule off with the whole rule in the body', async () => {
    const w = mount(AdminAutomodTab, { global })
    await flushPromises()
    await w.get('[data-rule] [data-testid="rule-toggle"]').setValue(false)
    await flushPromises()
    const put = http.calls.find(c => c.options?.method === 'PUT')
    expect(put?.url).toBe(`/api/admin/automod/rules/${fixtureId(1)}`)
    expect(put?.options?.json).toMatchObject({ kind: 'mentions', enabled: false, action: 'block', config: { max_mentions: 10 } })
  })

  it('deletes a rule only after the confirmation', async () => {
    const w = mount(AdminAutomodTab, { global })
    await flushPromises()
    confirmMock.mockResolvedValueOnce(false)
    await w.get('[data-testid="rule-delete"]').trigger('click')
    await flushPromises()
    expect(http.calls.some(c => c.options?.method === 'DELETE')).toBe(false)
    confirmMock.mockResolvedValueOnce(true)
    await w.get('[data-testid="rule-delete"]').trigger('click')
    await flushPromises()
    expect(http.calls.some(c => c.url === `/api/admin/automod/rules/${fixtureId(1)}` && c.options?.method === 'DELETE')).toBe(true)
  })

  it('tells when the rules cannot be loaded', async () => {
    http.handle = () => { throw new ApiError(500, 'INTERNAL_ERROR', 'x') }
    const w = mount(AdminAutomodTab, { global })
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('Couldn’t load the rules.')
    expect(useToastStore().toasts).toHaveLength(1)
  })

  it('announces a raid that is being held back', async () => {
    http.handle = url => (url === '/api/admin/automod' ? { rules, raid_until: new Date(Date.now() + 60_000).toISOString() } : {})
    const w = mount(AdminAutomodTab, { global })
    await flushPromises()
    expect(w.find('[data-testid="automod-raid-on"]').exists()).toBe(true)
  })
})

describe('AutomodRuleDialog', () => {
  async function openNew(kind: string) {
    const chat = useChatStore()
    chat.categories = [{ id: 'c', name: 'Main', sort_order: 0, channels: [{ id: fixtureId(50), name: 'general', type: 'text', topic: '', sort_order: 0, category_id: 'c', user_limit: 0 }] }] as never
    usePermissionsStore().roles = [{ id: fixtureId(60), name: 'Trusted', color: 0, position: 1, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null }]
    const w = mount(AdminAutomodTab, { global, attachTo: document.body })
    await flushPromises()
    await w.get(`[data-add="${kind}"]`).trigger('click')
    await flushPromises()
    return w
  }

  it('creates a word list rule with its exemptions', async () => {
    const w = await openNew('words')
    await w.get('[data-testid="am-words"]').setValue('spam\nbuy n*')
    await w.get('[data-testid="am-action-flag"]').setValue(true)
    await w.get('[data-testid="am-timeout"]').setValue('600')
    await w.get('[data-exempt-role="Trusted"]').setValue(true)
    await w.get('[data-exempt-channel="general"]').setValue(true)
    await w.get('[data-testid="automod-form"]').trigger('submit')
    await flushPromises()
    const post = http.calls.find(c => c.options?.method === 'POST')
    expect(post?.url).toBe('/api/admin/automod/rules')
    expect(post?.options?.json).toEqual({
      kind: 'words', name: 'Blocked words', enabled: true, action: 'flag', timeout_seconds: 600,
      config: { words: ['spam', 'buy n*'] }, exempt_roles: [fixtureId(60)], exempt_channels: [fixtureId(50)]
    })
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['Rule saved.'])
    expect(w.find('[data-testid="automod-form"]').exists()).toBe(false)
  })

  it('does not send a rule without a name and says why', async () => {
    const w = await openNew('mentions')
    await w.get('[data-testid="am-name"]').setValue('  ')
    expect(w.get('[data-testid="am-problem"]').text()).toBe('Give the rule a name.')
    expect(w.get('[data-testid="am-save"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="automod-form"]').trigger('submit')
    expect(http.calls.some(c => c.options?.method === 'POST')).toBe(false)
  })

  it('edits a rule in place and keeps a value outside the ready-made choices', async () => {
    rules = [rule(1, { kind: 'new_account', name: 'Young', config: { max_age_seconds: 7200, restrict: 'everything' } })]
    const w = mount(AdminAutomodTab, { global, attachTo: document.body })
    await flushPromises()
    await w.get('[data-testid="rule-edit"]').trigger('click')
    await flushPromises()
    expect((w.get('[data-testid="am-age"]').element as HTMLSelectElement).value).toBe('7200')
    await w.get('[data-testid="am-restrict"]').setValue('attachments')
    await w.get('[data-testid="automod-form"]').trigger('submit')
    await flushPromises()
    const put = http.calls.find(c => c.options?.method === 'PUT')
    expect(put?.options?.json).toMatchObject({ kind: 'new_account', config: { max_age_seconds: 7200, restrict: 'attachments' } })
  })

  it('shows a server refusal and stays open', async () => {
    const w = await openNew('mentions')
    http.handle = (url, o) => { if (o?.method === 'POST') throw new ApiError(409, 'CONFLICT', 'at most 25 automod rules'); return { rules } }
    await w.get('[data-testid="automod-form"]').trigger('submit')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.text).toContain('at most 25 automod rules')
    expect(w.find('[data-testid="automod-form"]').exists()).toBe(true)
  })

  it('has no action or timeout for raid protection', async () => {
    rules = []
    const w = await openNew('raid')
    expect(w.find('[data-testid="am-action-block"]').exists()).toBe(false)
    expect(w.find('[data-testid="am-timeout"]').exists()).toBe(false)
    expect(w.find('[data-testid="am-joins"]').exists()).toBe(true)
  })
})

describe('AdminDashboard automod tab', () => {
  function dashboard(bits: number, features: string[]) {
    useAuthStore().user = userFixture({ id: fixtureId(1) })
    usePermissionsStore().info = { owner: false, permissions: bits, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [] }
    useServerFeaturesStore().list = features
    return mount(AdminDashboard, { global, attachTo: document.body, shallow: false })
  }

  it('is for Administrators on a server that has automod', async () => {
    const w = dashboard(PERMISSION_BITS.administrator, ['automod'])
    await flushPromises()
    expect(document.querySelector('[data-testid="tab-automod"]')).not.toBeNull()
    w.unmount()
  })

  it('stays away from moderators with the queue permission only, and from older servers', async () => {
    const mod = dashboard(PERMISSION_BITS.manage_reports | PERMISSION_BITS.timeout_members, ['automod', 'reports'])
    await flushPromises()
    expect(document.querySelector('[data-testid="tab-automod"]')).toBeNull()
    mod.unmount()
    const old = dashboard(PERMISSION_BITS.administrator, [])
    await flushPromises()
    expect(document.querySelector('[data-testid="tab-automod"]')).toBeNull()
    old.unmount()
  })
})
