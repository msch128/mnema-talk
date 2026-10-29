import type { ApiOptions } from '../lib/api'
import type { DiscordimportPlan, DiscordimportRecord } from '../types/rest'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminImportTab from './AdminImportTab.vue'
import { ApiError } from '../lib/api'
import { setLocale } from '../i18n'
import { useToastStore } from '../stores/toast'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))
const confirmMock = vi.fn()
vi.mock('../lib/confirm', () => ({ confirm: (...args: unknown[]) => confirmMock(...args) }))

const IMPORT_ID = '00000000-0000-4000-8000-000000000701'

const plan: DiscordimportPlan = {
  code: 'AbCdEf123456',
  template_name: 'Gaming Hub',
  source_name: 'Gaming Hub Source',
  server_name: 'Home Base',
  categories: [{ source_id: '1', name: 'Info', overwrites: [] }],
  channels: [
    { source_id: '2', category_source_id: '1', category: 'Info', name: 'rules', type: 'text', source_type: 'text', topic: '', slow_mode_seconds: 0, user_limit: 0, private: false, overwrites: [], notes: [] },
    { source_id: '3', category_source_id: '', category: '', name: 'Lobby', type: 'voice', source_type: 'voice', topic: '', slow_mode_seconds: 0, user_limit: 5, private: true, overwrites: [], notes: ['a stage channel becomes a Talk'] }
  ],
  roles: [
    { source_id: '12', name: 'Moderator', source_name: 'Moderator', color: 65280, hoist: false, mentionable: true, permissions: 8, permission_keys: ['kick_members', 'ban_members'], dropped: ['MOVE_MEMBERS'], administrator_dropped: false, suggested_role_id: null },
    { source_id: '13', name: 'Admin', source_name: 'Admin', color: 0, hoist: false, mentionable: false, permissions: 16, permission_keys: ['manage_channels'], dropped: ['ADMINISTRATOR'], administrator_dropped: true, suggested_role_id: null }
  ],
  mapped_roles: [],
  mappable_roles: [],
  everyone: { permission_keys: ['view_channel'], dropped: [], applied: false },
  skipped: ['a thread (Discord channel type 11 has no counterpart)'],
  dropped_bits: ['ADMINISTRATOR', 'MOVE_MEMBERS'],
  administrator_dropped: true,
  counts: { categories: 1, channels: 2, roles: 2 },
  hash: 'abc123'
}

const record: DiscordimportRecord = {
  id: IMPORT_ID, template_code: 'AbCdEf123456', template_name: 'Gaming Hub', created_by_name: 'Owner',
  created_at: '2026-10-20T10:00:00Z', counts: { categories: 1, channels: 2, roles: 2 }, undone_at: null
}

let history: DiscordimportRecord[]
let applyError: Error | null
let channelsWithMessages: Array<{ name: string, messages: number }>

const writes = () => apiMock.mock.calls.filter(([path, o]) => o?.method === 'POST' && !path.endsWith('/preview'))
const body = (path: string) => apiMock.mock.calls.find(([p]) => p === path)?.[1]?.json

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset()
  history = []
  applyError = null
  channelsWithMessages = []
  apiMock.mockImplementation(async (path, options) => {
    if (path.endsWith('/preview')) return plan
    if (path.endsWith('/apply')) {
      if (applyError) throw applyError
      history = [record]
      return record
    }
    if (path.endsWith('/undo')) { history = [{ ...record, undone_at: '2026-10-21T10:00:00Z' }]; return { removed: record.counts, already_gone: { categories: 0, channels: 0, roles: 0 }, kept_categories: [] } }
    if (path.endsWith(`/imports/${IMPORT_ID}`)) {
      return { ...record, remaining: record.counts, channels_with_messages: channelsWithMessages, kept_categories: [] }
    }
    if (path.endsWith('/imports')) return history
    throw new Error(`unexpected ${options?.method ?? 'GET'} ${path}`)
  })
})

async function previewed() {
  const w = mount(AdminImportTab)
  await flushPromises()
  await w.get('[data-testid="import-link"]').setValue('https://discord.new/AbCdEf123456')
  await w.get('form').trigger('submit')
  await flushPromises()
  return w
}

describe('AdminImportTab', () => {
  it('previews the template without writing anything', async () => {
    const w = await previewed()
    expect(body('/api/admin/import/discord/preview')).toEqual({ template: 'https://discord.new/AbCdEf123456' })
    expect(writes()).toHaveLength(0)
    expect(w.findAll('[data-testid="import-channel"]').map(c => c.text())).toEqual([
      expect.stringContaining('rules'),
      expect.stringContaining('Lobby')
    ])
    expect(w.get('[data-testid="import-preview"]').text()).toContain('Without a category')
    const roles = w.findAll('[data-testid="import-role"]').map(r => r.text())
    expect(roles[0]).toContain('Kick members, Ban members')
    expect(roles[0]).toContain('Not imported: MOVE_MEMBERS')
    expect(w.get('[data-testid="import-admin-dropped"]').text()).toBe('Administrator is never imported.')
    expect(w.get('[data-testid="import-skipped"]').text()).toContain('a thread')
  })

  it('walks through three confirmations and only then imports', async () => {
    const w = await previewed()
    await w.get('[data-testid="import-review"]').trigger('click')

    // Step 1: what is added, and that nothing existing changes.
    expect(w.get('[data-testid="import-step-1"]').text()).toBe(
      'This adds 1 category, 2 channels and 2 roles at the end of your server. Nothing existing is changed, renamed or removed.'
    )
    await w.get('[data-testid="import-next"]').trigger('click')

    // Step 2: exactly which permissions, and what was dropped.
    const step2 = w.get('[data-testid="import-step-2"]').text()
    expect(step2).toContain('Moderator: Kick members, Ban members')
    expect(step2).toContain('Dropped, because Mnema Talk has no safe equivalent: ADMINISTRATOR, MOVE_MEMBERS')
    expect(step2).toContain('Discord’s Administrator permission is dropped from every role.')
    await w.get('[data-testid="import-next"]').trigger('click')

    // Step 3: type the name or IMPORT; nothing has been written until now.
    expect(writes()).toHaveLength(0)
    expect(w.get('[data-testid="import-step-3"]').text()).toContain('Type the server name “Home Base” or IMPORT')
    const run = w.get<HTMLButtonElement>('[data-testid="import-run"]')
    expect(run.element.disabled).toBe(true)
    await w.get('[data-testid="import-confirm-text"]').setValue('import')
    expect(run.element.disabled).toBe(true)
    await w.get('[data-testid="import-confirm-text"]').setValue('IMPORT')
    expect(run.element.disabled).toBe(false)
    await run.trigger('click')
    await flushPromises()

    expect(writes()).toHaveLength(1)
    expect(body('/api/admin/import/discord/apply')).toEqual({
      template: 'https://discord.new/AbCdEf123456', plan_hash: 'abc123',
      confirm_additive: true, confirm_roles: true, confirm_text: 'IMPORT'
    })
    expect(w.find('[data-testid="import-confirm"]').exists()).toBe(false)
    expect(w.find('[data-testid="import-preview"]').exists()).toBe(false)
    expect(w.findAll('[data-testid="import-record"]')).toHaveLength(1)
    expect(useToastStore().toasts.map(x => x.text)).toContain('Imported 1 category, 2 channels, 2 roles. You can undo it below.')
  })

  it('accepts the server name as the last confirmation and can go back a step', async () => {
    const w = await previewed()
    await w.get('[data-testid="import-review"]').trigger('click')
    await w.get('[data-testid="import-next"]').trigger('click')
    await w.get('[data-testid="import-next"]').trigger('click')
    await w.get('[data-testid="import-back"]').trigger('click')
    expect(w.find('[data-testid="import-step-2"]').exists()).toBe(true)
    await w.get('[data-testid="import-next"]').trigger('click')
    await w.get('[data-testid="import-confirm-text"]').setValue(' home base ')
    await w.get('[data-testid="import-run"]').trigger('click')
    await flushPromises()
    expect(body('/api/admin/import/discord/apply')).toMatchObject({ confirm_text: 'home base' })
  })

  it('cancelling any step writes nothing', async () => {
    const w = await previewed()
    await w.get('[data-testid="import-review"]').trigger('click')
    await w.get('[data-testid="import-cancel"]').trigger('click')
    expect(w.find('[data-testid="import-confirm"]').exists()).toBe(false)
    expect(writes()).toHaveLength(0)
  })

  it('asks for a new preview when the server says things changed', async () => {
    applyError = new ApiError(409, 'CONFLICT', 'the template or this server changed since the preview; preview it again')
    const w = await previewed()
    await w.get('[data-testid="import-review"]').trigger('click')
    await w.get('[data-testid="import-next"]').trigger('click')
    await w.get('[data-testid="import-next"]').trigger('click')
    await w.get('[data-testid="import-confirm-text"]').setValue('IMPORT')
    await w.get('[data-testid="import-run"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="import-error"]').exists()).toBe(false) // the dialog went away with the stale preview
    expect(w.find('[data-testid="import-preview"]').exists()).toBe(false)
    expect(useToastStore().toasts.map(x => x.text).join(' ')).toContain('Preview it again')
  })

  describe('undo', () => {
    beforeEach(() => { history = [record] })

    it('removes what the import created after one confirmation', async () => {
      confirmMock.mockResolvedValue(true)
      const w = mount(AdminImportTab)
      await flushPromises()
      await w.get('[data-testid="import-undo"]').trigger('click')
      await flushPromises()
      expect(confirmMock).toHaveBeenCalledTimes(1)
      expect(confirmMock.mock.calls[0]?.[0]).toMatchObject({ danger: true })
      expect(body(`/api/admin/import/discord/imports/${IMPORT_ID}/undo`)).toEqual({ accept_message_loss: false })
      expect(w.find('[data-testid="import-undo"]').exists()).toBe(false)
      expect(w.get('[data-testid="import-record"]').text()).toContain('Undone')
    })

    it('needs an extra confirmation for channels that have messages now', async () => {
      channelsWithMessages = [{ name: 'rules', messages: 12 }]
      confirmMock.mockResolvedValueOnce(true).mockResolvedValueOnce(false)
      const w = mount(AdminImportTab)
      await flushPromises()
      await w.get('[data-testid="import-undo"]').trigger('click')
      await flushPromises()
      expect(confirmMock).toHaveBeenCalledTimes(2)
      expect(confirmMock.mock.calls[1]?.[0].body).toContain('rules (12)')
      expect(writes()).toHaveLength(0)

      confirmMock.mockReset().mockResolvedValue(true)
      await w.get('[data-testid="import-undo"]').trigger('click')
      await flushPromises()
      expect(body(`/api/admin/import/discord/imports/${IMPORT_ID}/undo`)).toEqual({ accept_message_loss: true })
    })

    it('says when roles stay because the admin can no longer manage them', async () => {
      confirmMock.mockResolvedValue(true)
      const base = apiMock.getMockImplementation()
      apiMock.mockImplementation(async (path, options) => {
        const res = await base?.(path, options)
        return path.endsWith('/undo') ? { ...(res as object), kept_roles: 2 } : res
      })
      const w = mount(AdminImportTab)
      await flushPromises()
      await w.get('[data-testid="import-undo"]').trigger('click')
      await flushPromises()
      expect(useToastStore().toasts.map(x => x.text).join(' ')).toContain('2 role(s) stay because you can no longer manage them.')
    })

    it('does nothing when the first confirmation is declined', async () => {
      confirmMock.mockResolvedValue(false)
      const w = mount(AdminImportTab)
      await flushPromises()
      await w.get('[data-testid="import-undo"]').trigger('click')
      await flushPromises()
      expect(writes()).toHaveLength(0)
    })
  })
})
