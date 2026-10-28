import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ReportDialog from './ReportDialog.vue'
import AdminReportsTab from './AdminReportsTab.vue'
import AccountDmPolicy from './AccountDmPolicy.vue'
import AccountBlockedList from './AccountBlockedList.vue'
import AdminOnboardingTab from './AdminOnboardingTab.vue'
import AdminDashboard from './AdminDashboard.vue'
import { closeReport, openReport } from '../lib/reportDialog'
import { useAuthStore } from '../stores/auth'
import { useBlocksStore } from '../stores/blocks'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import type { ApiOptions } from '../lib/api'
import { ApiError } from '../lib/api'
import { PERMISSION_BITS } from '../lib/permissions'

const http = vi.hoisted(() => ({ handle: (_url: string, _opts?: ApiOptions): unknown => [] }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => {
    try { return Promise.resolve(http.handle(url, options)) } catch (error) { return Promise.reject(error) }
  })
}))
const confirmMock = vi.hoisted(() => vi.fn())
vi.mock('../lib/confirm', () => ({ confirm: confirmMock }))

const global = { plugins: [i18nPlugin], directives: { tooltip } }
const party = (n: number, username: string) => ({ id: fixtureId(n), username })
const report = (id: number, extra: Record<string, unknown> = {}) => ({
  id: fixtureId(id), category: 'harassment', kind: 'dm', message_id: fixtureId(id + 100), note: 'rude', status: 'open',
  created_at: '2026-01-02T03:04:05Z', reporter: party(1, 'ada'), reported: party(2, 'bo'), ...extra
})
const detail = (id: number, extra: Record<string, unknown> = {}) => ({
  ...report(id, extra),
  snapshot: {
    message: { id: fixtureId(id + 100), user_id: fixtureId(2), username: 'bo', display_name: 'Bo', content: 'bad words', created_at: '2026-01-02T03:00:00Z', attachments: [{ filename: 'pic.png', mime_type: 'image/png', size_bytes: 3 }] },
    // As the server keeps it: the neighbours only, the reported message apart.
    context: [
      { id: fixtureId(id + 101), user_id: fixtureId(1), username: 'ada', display_name: 'Ada', content: 'after', created_at: '2026-01-02T03:01:00Z', attachments: [] },
      { id: fixtureId(id + 99), user_id: fixtureId(1), username: 'ada', display_name: 'Ada', content: 'before', created_at: '2026-01-02T02:59:00Z', attachments: [] }
    ]
  }
})

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  http.handle = () => []
  confirmMock.mockReset()
  closeReport()
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})
afterEach(() => {
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('ReportDialog', () => {
  it('renders nothing until a report is opened', () => {
    expect(mount(ReportDialog, { global }).find('[data-testid="report-dialog"]').exists()).toBe(false)
  })

  it('sends the reason and the note for the message and thanks the reporter', async () => {
    let sent: { url: string; json: unknown } | undefined
    http.handle = (url, o) => { sent = { url, json: o?.json }; return { id: fixtureId(9) } }
    const w = mount(ReportDialog, { global, attachTo: document.body })
    openReport({ messageId: fixtureId(5), author: 'Bo' })
    await flushPromises()
    expect(w.text()).toContain('message from Bo')
    await w.get('[data-report-category="harassment"]').setValue(true)
    await w.get('[data-testid="report-note"]').setValue('  rude  ')
    await w.get('[data-testid="report-dialog"]').trigger('submit')
    await flushPromises()
    expect(sent).toEqual({ url: `/api/messages/${fixtureId(5)}/report`, json: { category: 'harassment', note: 'rude' } })
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['Report sent. The moderators will look at it.'])
    expect(w.find('[data-testid="report-dialog"]').exists()).toBe(false)
  })

  it.each([
    [404, 'Couldn’t send the report. The message is no longer there.'],
    [409, 'You have too many open reports. Wait until the moderators have looked at them.'],
    [429, 'Too many reports in a short time. Try again later.'],
    [500, 'Couldn’t send the report. Try again.']
  ])('explains a %i answer and stays open', async (status, text) => {
    http.handle = () => { throw new ApiError(status, 'X', 'no') }
    const w = mount(ReportDialog, { global, attachTo: document.body })
    openReport({ messageId: fixtureId(5), author: 'Bo' })
    await flushPromises()
    await w.get('[data-testid="report-dialog"]').trigger('submit')
    await flushPromises()
    expect(w.get('[data-testid="report-error"]').text()).toBe(text)
  })

  it('starts every report from a clean form', async () => {
    const w = mount(ReportDialog, { global, attachTo: document.body })
    openReport({ messageId: fixtureId(5), author: 'Bo' })
    await flushPromises()
    await w.get('[data-testid="report-note"]').setValue('first')
    openReport({ messageId: fixtureId(6), author: 'Cy' })
    await flushPromises()
    expect((w.get('[data-testid="report-note"]').element as HTMLTextAreaElement).value).toBe('')
  })
})

describe('AdminReportsTab', () => {
  it('lists the open reports without their text and filters by status', async () => {
    const urls: string[] = []
    http.handle = url => { urls.push(url); return { has_more: false, reports: [report(10), report(11, { kind: 'channel', channel_name: 'general' })] } }
    const w = mount(AdminReportsTab, { global })
    await flushPromises()
    expect(urls[0]).toBe('/api/admin/reports?status=open&limit=25')
    const rows = w.findAll('[data-report]')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.text()).toContain('Direct message')
    expect(rows[0]!.text()).toContain('ada reported bo')
    expect(rows[1]!.text()).toContain('#general')
    expect(w.text()).not.toContain('bad words')
    await w.get('[data-filter="resolved"]').trigger('click')
    await flushPromises()
    expect(urls[1]).toBe('/api/admin/reports?status=resolved&limit=25')
  })

  it('says when there is nothing to do and loads more pages', async () => {
    http.handle = () => ({ has_more: false, reports: [] })
    const empty = mount(AdminReportsTab, { global })
    await flushPromises()
    expect(empty.get('[data-testid="reports-empty"]').text()).toContain('No reports here.')

    const urls: string[] = []
    http.handle = url => { urls.push(url); return { has_more: urls.length === 1, reports: [report(urls.length === 1 ? 10 : 12)] } }
    const w = mount(AdminReportsTab, { global })
    await flushPromises()
    await w.get('[data-testid="reports-more"]').trigger('click')
    await flushPromises()
    expect(urls[1]).toBe(`/api/admin/reports?status=open&limit=25&before=${fixtureId(10)}`)
    expect(w.findAll('[data-report]')).toHaveLength(2)
  })

  it('reports a failed load', async () => {
    http.handle = () => { throw new Error('offline') }
    mount(AdminReportsTab, { global })
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['offline'])
  })

  it('opens a report with the copy of the messages and resolves it with a note', async () => {
    const seen: { url: string; method: string | undefined; json: unknown }[] = []
    http.handle = (url, o) => {
      seen.push({ url, method: o?.method, json: o?.json })
      if (url === `/api/admin/reports/${fixtureId(10)}`) return detail(10)
      if (url.endsWith('/resolve')) return report(10, { status: 'resolved' })
      return { has_more: false, reports: [report(10)] }
    }
    const w = mount(AdminReportsTab, { global })
    await flushPromises()
    await w.get(`[data-report="${fixtureId(10)}"]`).trigger('click')
    await flushPromises()
    const copy = w.get('[data-testid="report-context"]').text()
    expect(copy.indexOf('before')).toBeLessThan(copy.indexOf('bad words'))
    expect(copy.indexOf('bad words')).toBeLessThan(copy.indexOf('after'))
    expect(w.get('[data-reported="true"]').text()).toContain('bad words')
    expect(w.text()).toContain('only part of a direct message')
    expect(w.text()).toContain('pic.png')
    await w.get('[data-testid="report-resolution"]').setValue('warned')
    await w.get('[data-testid="report-resolve"]').trigger('click')
    await flushPromises()
    expect(seen.find(c => c.url.endsWith('/resolve'))).toEqual({ url: `/api/admin/reports/${fixtureId(10)}/resolve`, method: 'POST', json: { status: 'resolved', note: 'warned' } })
    expect(w.find('[data-testid="report-detail"]').exists()).toBe(false)
    expect(useToastStore().toasts.map(t => t.text)).toContain('Report marked resolved.')
  })

  it('dismisses a report, and deletes a closed one after asking', async () => {
    let current = detail(10)
    const seen: string[] = []
    http.handle = (url, o) => {
      seen.push(`${o?.method ?? 'GET'} ${url}`)
      if (url === `/api/admin/reports/${fixtureId(10)}` && !o?.method) return current
      return { has_more: false, reports: [report(10)] }
    }
    const w = mount(AdminReportsTab, { global })
    await flushPromises()
    await w.get(`[data-report="${fixtureId(10)}"]`).trigger('click')
    await flushPromises()
    await w.get('[data-testid="report-dismiss"]').trigger('click')
    await flushPromises()
    expect(seen).toContain(`POST /api/admin/reports/${fixtureId(10)}/resolve`)

    current = detail(10, { status: 'dismissed', resolved_by: party(3, 'moddy'), resolved_at: '2026-01-03T00:00:00Z', resolution_note: 'no action' })
    await w.get(`[data-report="${fixtureId(10)}"]`).trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="report-closed"]').text()).toContain('Dismissed by moddy')
    confirmMock.mockResolvedValueOnce(false)
    await w.get('[data-testid="report-delete"]').trigger('click')
    await flushPromises()
    expect(seen).not.toContain(`DELETE /api/admin/reports/${fixtureId(10)}`)
    confirmMock.mockResolvedValueOnce(true)
    await w.get('[data-testid="report-delete"]').trigger('click')
    await flushPromises()
    expect(seen).toContain(`DELETE /api/admin/reports/${fixtureId(10)}`)
  })

  it('goes back to the list and tells when a report cannot be read', async () => {
    http.handle = url => {
      if (url === `/api/admin/reports/${fixtureId(10)}`) throw new Error('gone')
      return { has_more: false, reports: [report(10)] }
    }
    const w = mount(AdminReportsTab, { global })
    await flushPromises()
    await w.get(`[data-report="${fixtureId(10)}"]`).trigger('click')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('Couldn’t load the reports. Try again.')
    await w.get('[data-testid="report-back"]').trigger('click')
    expect(w.find('[data-report]').exists()).toBe(true)
  })
})

describe('report detail layout', () => {
  it('quotes the reported message first and keeps the closing actions in a sticky footer', async () => {
    http.handle = url => {
      if (url === `/api/admin/reports/${fixtureId(10)}`) return detail(10, { kind: 'channel', channel_name: 'general' })
      return { has_more: false, reports: [report(10, { kind: 'channel', channel_name: 'general' })] }
    }
    const w = mount(AdminReportsTab, { global })
    await flushPromises()
    await w.get(`[data-report="${fixtureId(10)}"]`).trigger('click')
    await flushPromises()
    const html = w.html()
    expect(w.get('[data-testid="report-quote"]').text()).toContain('bad words')
    expect(html.indexOf('data-testid="report-quote"')).toBeLessThan(html.indexOf('data-testid="report-context"'))
    expect(w.get('[data-testid="report-actions"]').classes()).toContain('sticky')
    expect(w.get('[data-testid="report-actions"]').find('[data-testid="report-resolve"]').exists()).toBe(true)
  })

  it('offers no sanction to someone without timeout or ban rights', async () => {
    usePermissionsStore().info = { owner: false, permissions: PERMISSION_BITS.manage_reports, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [] }
    http.handle = url => (url === `/api/admin/reports/${fixtureId(10)}` ? detail(10) : { has_more: false, reports: [report(10)] })
    const w = mount(AdminReportsTab, { global })
    await flushPromises()
    await w.get(`[data-report="${fixtureId(10)}"]`).trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="report-sanction"]').exists()).toBe(false)
  })
})

describe('AdminDashboard reports tab', () => {
  it('counts the open reports beside the tab', async () => {
    useAuthStore().user = userFixture({ id: fixtureId(1) })
    usePermissionsStore().info = { owner: false, permissions: PERMISSION_BITS.manage_reports, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [] }
    useServerFeaturesStore().list = ['reports']
    http.handle = url => (url.startsWith('/api/admin/reports') ? { has_more: false, reports: [report(10), report(11)] } : [])
    const w = mount(AdminDashboard, { global, attachTo: document.body, shallow: false })
    await flushPromises()
    expect(document.querySelector('[data-badge="reports"]')?.textContent).toBe('2')
    w.unmount()
  })

  it('shows the tab to a member who manages reports on a server that has them', async () => {
    useAuthStore().user = userFixture({ id: fixtureId(1) })
    usePermissionsStore().info = { owner: false, permissions: PERMISSION_BITS.manage_reports, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [] }
    const features = useServerFeaturesStore()
    features.list = ['reports']
    http.handle = url => (url.startsWith('/api/admin/reports') ? { has_more: false, reports: [] } : [])
    const w = mount(AdminDashboard, { global, attachTo: document.body, shallow: false })
    await flushPromises()
    expect(document.querySelector('[data-testid="tab-reports"]')).not.toBeNull()
    features.list = []
    await flushPromises()
    expect(document.querySelector('[data-testid="tab-reports"]')).toBeNull()
    w.unmount()
  })
})

describe('AccountDmPolicy', () => {
  it('shows the stored choice and saves a change', async () => {
    let stored = 'everyone'
    http.handle = (url, o) => {
      if (o?.method === 'PUT') stored = (o.json as { dm_policy: string }).dm_policy
      return { dm_policy: stored }
    }
    const w = mount(AccountDmPolicy, { global })
    await flushPromises()
    expect((w.get('[data-dm-policy="everyone"]').element as HTMLInputElement).checked).toBe(true)
    await w.get('[data-dm-policy="nobody"]').trigger('change')
    await flushPromises()
    expect(stored).toBe('nobody')
    expect((w.get('[data-dm-policy="nobody"]').element as HTMLInputElement).checked).toBe(true)
  })

  it('goes back to the old choice when saving fails, and stays hidden when it cannot be read', async () => {
    http.handle = (_url, o) => { if (o?.method === 'PUT') throw new Error('offline'); return { dm_policy: 'everyone' } }
    const w = mount(AccountDmPolicy, { global })
    await flushPromises()
    await w.get('[data-dm-policy="nobody"]').trigger('change')
    await flushPromises()
    expect((w.get('[data-dm-policy="everyone"]').element as HTMLInputElement).checked).toBe(true)
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['Couldn’t save the setting. Try again.'])
    http.handle = () => { throw new Error('offline') }
    expect(mount(AccountDmPolicy, { global }).find('[data-testid="dm-policy"]').exists()).toBe(false)
  })

  it('tells when the choice cannot be read and loads it on retry', async () => {
    http.handle = () => { throw new Error('offline') }
    const w = mount(AccountDmPolicy, { global })
    await flushPromises()
    expect(w.find('[data-testid="dm-policy"]').exists()).toBe(false)
    expect(w.get('[role="alert"]').text()).toBe('Couldn’t load the setting. Try again.')
    http.handle = () => ({ dm_policy: 'nobody' })
    await w.get('[data-testid="dm-policy-retry"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="dm-policy-failed"]').exists()).toBe(false)
    expect((w.get('[data-dm-policy="nobody"]').element as HTMLInputElement).checked).toBe(true)
  })
})

describe('AdminReportDetail variants', () => {
  it('shows a channel report without the direct message notice, and a closed one without a recorded actor', async () => {
    const channelDetail = detail(20, { kind: 'channel', channel_name: 'general', note: '', status: 'resolved', resolved_by: undefined, resolved_at: undefined, reporter: undefined })
    http.handle = url => (url === `/api/admin/reports/${fixtureId(20)}` ? channelDetail : { has_more: false, reports: [report(20, { kind: 'channel', channel_name: 'general' })] })
    const w = mount(AdminReportsTab, { global })
    await flushPromises()
    await w.get(`[data-report="${fixtureId(20)}"]`).trigger('click')
    await flushPromises()
    expect(w.text()).toContain('#general')
    expect(w.text()).not.toContain('only part of a direct message')
    expect(w.find('[data-testid="report-note-text"]').exists()).toBe(false)
    expect(w.get('[data-testid="report-closed"]').text()).toContain('Resolved by')
  })

  it('tells when closing or deleting a report fails', async () => {
    http.handle = (url, o) => {
      if (o?.method) throw new Error('refused')
      return url === `/api/admin/reports/${fixtureId(10)}` ? detail(10) : { has_more: false, reports: [report(10)] }
    }
    const w = mount(AdminReportsTab, { global })
    await flushPromises()
    await w.get(`[data-report="${fixtureId(10)}"]`).trigger('click')
    await flushPromises()
    await w.get('[data-testid="report-resolve"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['refused'])
    expect(w.find('[data-testid="report-detail"]').exists()).toBe(true)
  })
})

describe('AccountBlockedList', () => {
  it('lists the blocked members with a way to lift each block', async () => {
    useServerFeaturesStore().list = ['blocks']
    const blocks = useBlocksStore()
    blocks.blocked = [{ user_id: fixtureId(2), username: 'ada', display_name: 'Ada', blocked_at: '2026-01-01T00:00:00Z' }]
    http.handle = url => (url === '/api/me/blocks' ? [] : undefined)
    const w = mount(AccountBlockedList, { global })
    expect(w.get('[data-blocked="ada"]').text()).toContain('Ada')
    await w.get('[data-testid="blocked-unblock"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="blocked-empty"]').text()).toBe('You haven’t blocked anyone.')
  })

  it('falls back to the username for a member without a display name', () => {
    useServerFeaturesStore().list = ['blocks']
    useBlocksStore().blocked = [{ user_id: fixtureId(3), username: 'cy', display_name: '', avatar_url: '/a.png', blocked_at: '2026-01-01T00:00:00Z' }]
    expect(mount(AccountBlockedList, { global }).get('[data-blocked="cy"]').text()).toContain('cy')
  })
})

describe('the kill switch in the welcome settings', () => {
  const settings = { community_name: 'Home', welcome_text: '', rules_text: '', rules_version: 0, start_channel_id: null, direct_messages_enabled: true, session_inactivity_days: 7 }

  it('is offered on a server with direct messages and saved with the other settings', async () => {
    useServerFeaturesStore().list = ['dm']
    const puts: unknown[] = []
    http.handle = (_url, o) => { if (o?.method === 'PUT') { puts.push(o.json); return { ...settings, direct_messages_enabled: false } } return settings }
    const w = mount(AdminOnboardingTab, { global })
    await flushPromises()
    expect(w.get('[data-testid="onboarding-save"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="dm-enabled"]').setValue(false)
    await w.get('[data-testid="onboarding-save"]').trigger('submit')
    await flushPromises()
    expect(puts[0]).toMatchObject({ direct_messages_enabled: false })
  })

  it('is absent, and not sent, on a server without direct messages', async () => {
    const puts: unknown[] = []
    http.handle = (_url, o) => { if (o?.method === 'PUT') { puts.push(o.json); return settings } return settings }
    const w = mount(AdminOnboardingTab, { global })
    await flushPromises()
    expect(w.find('[data-testid="dm-switch"]').exists()).toBe(false)
    await w.get('#onb-name').setValue('Renamed')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(puts[0]).not.toHaveProperty('direct_messages_enabled')
  })
})
