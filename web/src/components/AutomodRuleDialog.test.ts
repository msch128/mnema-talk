import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AutomodRuleDialog from './AutomodRuleDialog.vue'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { fixtureId } from '../test-fixtures.fixture'
import type { ApiOptions } from '../lib/api'
import type { AutomodRule } from '../types/rest'

const http = vi.hoisted(() => ({ calls: [] as Array<{ url: string; options?: ApiOptions }>, fail: false }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => {
    http.calls.push({ url, ...(options ? { options } : {}) })
    if (http.fail) return Promise.reject(new Error('boom'))
    return Promise.resolve({ ...(options?.json as object), id: fixtureId(9), updated_at: '2026-01-01T00:00:00Z' })
  })
}))

const global = { plugins: [i18nPlugin], directives: { tooltip } }
const role = (n: number, name: string, extra: Record<string, unknown> = {}) => ({
  id: fixtureId(n), name, color: 0, position: 1, permissions: 0, hoist: false, mentionable: false, is_everyone: false, managed: false, max_upload_mb: null, ...extra
})
function ruleOf(extra: Partial<AutomodRule> = {}): AutomodRule {
  return {
    id: fixtureId(5), kind: 'links', name: 'Links', enabled: true, action: 'block', timeout_seconds: 0,
    config: { mode: 'allow_list', domains: ['example.com'] }, exempt_roles: [fixtureId(60)], exempt_channels: [], updated_at: '2026-01-01T00:00:00Z', ...extra
  } as AutomodRule
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  http.calls = []
  http.fail = false
  useChatStore().categories = [{ id: 'c', name: 'Main', sort_order: 0, channels: [{ id: fixtureId(50), name: 'general', type: 'text', topic: '', sort_order: 0, category_id: 'c', user_limit: 0 }] }] as never
  usePermissionsStore().roles = [role(60, 'Trusted'), role(61, 'Everyone', { is_everyone: true }), role(62, 'Bot role', { managed: true })] as never
})
afterEach(() => { document.body.innerHTML = '' })

function open(props: Record<string, unknown>) {
  return mount(AutomodRuleDialog, { props, global, attachTo: document.body })
}
const lastCall = () => http.calls.at(-1)

describe('AutomodRuleDialog', () => {
  it('lists only assignable roles as exemptions', () => {
    const w = open({ kind: 'words' })
    expect(w.findAll('[data-exempt-role]').map(r => r.attributes('data-exempt-role'))).toEqual(['Trusted'])
    w.unmount()
  })

  it('tells when there are no roles to exempt', () => {
    usePermissionsStore().roles = []
    const w = open({ kind: 'words' })
    expect(w.find('fieldset p').exists()).toBe(true)
    expect(w.findAll('[data-exempt-role]')).toHaveLength(0)
    w.unmount()
  })

  it('shows the domain list only when links are not blocked wholesale, and sends it', async () => {
    const w = open({ kind: 'links' })
    await w.get('[data-testid="am-mode"]').setValue('block_all')
    expect(w.find('[data-testid="am-domains"]').exists()).toBe(false)
    await w.get('[data-testid="am-mode"]').setValue('allow_list')
    await w.get('[data-testid="am-domains"]').setValue('a.com\n\n b.org ')
    await w.get('[data-testid="automod-form"]').trigger('submit')
    await flushPromises()
    expect(lastCall()?.options?.json).toMatchObject({ kind: 'links', config: { mode: 'allow_list', domains: ['a.com', 'b.org'] } })
    expect(w.emitted('saved')).toHaveLength(1)
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })

  it('refuses too many words and too many domains', async () => {
    const words = open({ kind: 'words' })
    await words.get('[data-testid="am-words"]').setValue(Array.from({ length: 201 }, (_, i) => `w${i}`).join('\n'))
    expect(words.get('[data-testid="am-problem"]').text()).toContain('200')
    words.unmount()
    const links = open({ kind: 'links' })
    await links.get('[data-testid="am-domains"]').setValue(Array.from({ length: 101 }, (_, i) => `d${i}.com`).join('\n'))
    expect(links.get('[data-testid="am-problem"]').text()).toContain('100')
    await links.get('[data-testid="am-mode"]').setValue('block_all')
    expect(links.find('[data-testid="am-problem"]').exists()).toBe(false)
    links.unmount()
  })

  it('rejects an invalid mention limit and sends a valid one', async () => {
    const w = open({ kind: 'mentions' })
    await w.get('[data-testid="am-mentions"]').setValue('0')
    expect(w.find('[data-testid="am-problem"]').exists()).toBe(true)
    await w.get('[data-testid="am-mentions"]').setValue('51')
    expect(w.find('[data-testid="am-problem"]').exists()).toBe(true)
    await w.get('[data-testid="am-mentions"]').setValue('7')
    expect(w.find('[data-testid="am-problem"]').exists()).toBe(false)
    await w.get('[data-testid="automod-form"]').trigger('submit')
    await flushPromises()
    expect(lastCall()?.options?.json).toMatchObject({ config: { max_mentions: 7 } })
    w.unmount()
  })

  it('sends the raid settings and always flags without a timeout', async () => {
    const w = open({ kind: 'raid' })
    await w.get('[data-testid="am-joins"]').setValue('1')
    expect(w.find('[data-testid="am-problem"]').exists()).toBe(true)
    await w.get('[data-testid="am-joins"]').setValue('25')
    await w.get('[data-testid="am-window"]').setValue('60')
    await w.get('[data-testid="am-restrict-time"]').setValue('3600')
    await w.get('[data-testid="automod-form"]').trigger('submit')
    await flushPromises()
    expect(lastCall()?.options?.json).toMatchObject({
      kind: 'raid', action: 'flag', timeout_seconds: 0, config: { joins: 25, window_seconds: 60, restrict_seconds: 3600 }
    })
    w.unmount()
  })

  it('edits an existing rule via PUT, toggling exemptions and the enabled state', async () => {
    const w = open({ rule: ruleOf() })
    expect((w.get('[data-exempt-role="Trusted"]').element as HTMLInputElement).checked).toBe(true)
    await w.get('[data-exempt-role="Trusted"]').setValue(false)
    await w.get('[data-exempt-channel="general"]').setValue(true)
    await w.get('[data-testid="am-enabled"]').trigger('click')
    await w.get('[data-testid="am-action-flag"]').setValue(true)
    await w.get('[data-testid="automod-form"]').trigger('submit')
    await flushPromises()
    expect(lastCall()?.url).toBe(`/api/admin/automod/rules/${fixtureId(5)}`)
    expect(lastCall()?.options?.method).toBe('PUT')
    expect(lastCall()?.options?.json).toMatchObject({ enabled: false, action: 'flag', exempt_roles: [], exempt_channels: [fixtureId(50)] })
    w.unmount()
  })

  it('shows the new-account choices and sends them', async () => {
    const w = open({ kind: 'new_account' })
    await w.get('[data-testid="am-age"]').setValue('3600')
    await w.get('[data-testid="am-restrict"]').setValue('everything')
    await w.get('[data-testid="automod-form"]').trigger('submit')
    await flushPromises()
    expect(lastCall()?.options?.json).toMatchObject({ kind: 'new_account', config: { max_age_seconds: 3600, restrict: 'everything' } })
    w.unmount()
  })

  it('reports a failed save with a toast and stays open', async () => {
    http.fail = true
    const w = open({ kind: 'mentions' })
    await w.get('[data-testid="automod-form"]').trigger('submit')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.text).toBe('boom')
    expect(w.emitted('close')).toBeUndefined()
    expect(w.get('[data-testid="am-save"]').attributes('disabled')).toBeUndefined()
    w.unmount()
  })

  it('closes on cancel', async () => {
    const w = open({ kind: 'words' })
    await w.findAll('button').find(b => b.text() === 'Cancel')!.trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })
})
