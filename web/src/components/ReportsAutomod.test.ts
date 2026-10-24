import type { ApiOptions } from '../lib/api'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminReportsTab from './AdminReportsTab.vue'
import AdminReportDetail from './AdminReportDetail.vue'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { fixtureId } from '../test-fixtures.fixture'

const http = vi.hoisted(() => ({ handle: (_url: string, _o?: ApiOptions): unknown => ({}) }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => Promise.resolve(http.handle(url, options)))
}))

const global = { plugins: [i18nPlugin], directives: { tooltip } }
const entry = (n: number, extra: Record<string, unknown> = {}) => ({
  id: fixtureId(n), kind: 'channel', category: 'automod', status: 'open', note: '', source: 'automod', automod_rule: 'No spam',
  automod_outcome: 'blocked', hit_count: 1, channel_id: fixtureId(40), channel_name: 'general', created_at: '2026-01-02T03:04:05Z',
  reported: { id: fixtureId(2), username: 'bo' }, ...extra
})
const snapshot = (content: string) => ({
  message: { id: '00000000-0000-0000-0000-000000000000', user_id: fixtureId(2), username: 'bo', display_name: 'Bo', content, created_at: '2026-01-02T03:04:05Z', attachments: [] },
  context: []
})

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})
afterEach(() => { document.body.innerHTML = '' })

describe('automod entries in the queue', () => {
  it('lists them marked Automod, with the rule, the outcome and the count', async () => {
    http.handle = () => ({ has_more: false, reports: [
      entry(1, { hit_count: 4 }),
      entry(2, { automod_outcome: 'flagged' }),
      entry(3, { kind: 'alert', reported: undefined, channel_id: undefined, channel_name: undefined, automod_outcome: 'alert', automod_rule: 'Raid protection', hit_count: 5 }),
      entry(4, { kind: 'dm', channel_id: undefined, channel_name: undefined })
    ] })
    const w = mount(AdminReportsTab, { global })
    await flushPromises()
    const rows = w.findAll('[data-report]').map(r => r.text())
    expect(rows[0]).toContain('Automod')
    expect(rows[0]).toContain('“No spam” blocked a message of bo ×4')
    expect(rows[0]).toContain('#general')
    expect(rows[1]).toContain('“No spam” flagged a message of bo')
    expect(rows[1]).not.toContain('×')
    expect(rows[2]).toContain('“Raid protection” restricted newcomers ×5')
    expect(rows[2]).toContain('Server')
    expect(rows[3]).toContain('Direct message')
  })

  it('opens a blocked message with its text and the note that it was never stored', async () => {
    http.handle = () => ({ ...entry(1), snapshot: snapshot('the refused words') })
    const w = mount(AdminReportDetail, { props: { id: fixtureId(1) }, global })
    await flushPromises()
    expect(w.get('[data-testid="report-automod-rule"]').text()).toBe('No spam · Blocked')
    expect(w.text()).not.toContain('Reported by')
    expect(w.get('[data-testid="report-context"]').text()).toContain('the refused words')
    expect(w.text()).toContain('never stored')
  })

  it('keeps no copy for an automod hit in a direct message', async () => {
    http.handle = () => ({ ...entry(1, { kind: 'dm', channel_id: undefined, channel_name: undefined }), snapshot: snapshot('') })
    const w = mount(AdminReportDetail, { props: { id: fixtureId(1) }, global })
    await flushPromises()
    expect(w.find('[data-testid="report-context"]').exists()).toBe(false)
    expect(w.get('[data-testid="report-dm-automod"]').text()).toContain('never keeps the text of a direct message')
  })

  it('explains a raid alert and has no message', async () => {
    http.handle = () => ({ ...entry(1, { kind: 'alert', reported: undefined, channel_id: undefined, channel_name: undefined, automod_outcome: 'alert', automod_rule: 'Raid protection', hit_count: 3 }), snapshot: snapshot('') })
    const w = mount(AdminReportDetail, { props: { id: fixtureId(1) }, global })
    await flushPromises()
    expect(w.find('[data-testid="report-context"]').exists()).toBe(false)
    expect(w.get('[data-testid="report-alert-text"]').text()).toContain('many accounts joining')
    expect(w.get('[data-testid="report-automod-rule"]').text()).toBe('Raid protection · Raid alert · 3 hits')
    // It closes like any report.
    expect(w.find('[data-testid="report-resolve"]').exists()).toBe(true)
  })
})
