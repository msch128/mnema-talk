import type { ApiOptions as BaseApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import EditHistoryDialog from './EditHistoryDialog.vue'
import { setLocale } from '../i18n'
import { openEditHistory, closeEditHistory, pendingEditHistory } from '../lib/editHistory'

type ApiOptions = BaseApiOptions & { decode?: (value: unknown) => unknown }
const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const request = { channelId: 'c1', messageId: 'm1', current: 'third wording', editedAt: '2026-01-03T10:00:00Z' }
const version = (id: number, content: string) => ({
  id, content, edited_at: '2026-01-02T10:00:00Z', replaced_at: '2026-01-03T10:00:00Z', editor_id: '6f1c2d3e-4a5b-4c6d-8e7f-0a1b2c3d4e5f'
})

beforeEach(() => {
  setLocale('en')
  apiMock.mockReset()
  closeEditHistory()
})

describe('EditHistoryDialog', () => {
  it('renders nothing until asked', () => {
    expect(mount(EditHistoryDialog).find('[data-testid="edit-history-list"]').exists()).toBe(false)
  })

  it('shows the current text and the earlier versions as plain text', async () => {
    apiMock.mockImplementation(async (_path, options) => options?.decode?.({ edits: [version(2, '<b>second</b> wording'), version(1, 'first wording')] }))
    const w = mount(EditHistoryDialog)
    openEditHistory(request)
    await flushPromises()
    expect(apiMock.mock.calls[0]?.[0]).toBe('/api/channels/c1/messages/m1/edits')
    const versions = w.findAll('[data-testid="edit-history-version"]')
    expect(versions).toHaveLength(2)
    expect(versions[0]?.text()).toContain('<b>second</b> wording')
    expect(versions[0]?.find('b').exists()).toBe(false)
    expect(w.text()).toContain('third wording')
    expect(w.text()).toContain('Current')
  })

  it('says so when no earlier version is kept', async () => {
    apiMock.mockImplementation(async (_path, options) => options?.decode?.({ edits: [] }))
    const w = mount(EditHistoryDialog)
    openEditHistory(request)
    await flushPromises()
    expect(w.text()).toContain('No earlier versions are kept')
  })

  it('reports a failure instead of an empty list', async () => {
    apiMock.mockRejectedValue(new Error('Forbidden'))
    const w = mount(EditHistoryDialog)
    openEditHistory(request)
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('Forbidden')
    expect(w.text()).not.toContain('No earlier versions')
  })

  it('ignores a slow answer for a message that is no longer shown', async () => {
    let release: (v: unknown) => void = () => {}
    apiMock.mockImplementation((path, options) => {
      if (path.includes('/m1/')) return new Promise(resolve => { release = resolve })
      return Promise.resolve(options?.decode?.({ edits: [version(9, 'newer message version')] }))
    })
    const w = mount(EditHistoryDialog)
    openEditHistory(request)
    await nextTick()
    openEditHistory({ ...request, messageId: 'm2' })
    await flushPromises()
    release({ edits: [version(1, 'stale version')] })
    await flushPromises()
    expect(w.text()).toContain('newer message version')
    expect(w.text()).not.toContain('stale version')
  })

  it('closes with the button', async () => {
    apiMock.mockImplementation(async (_path, options) => options?.decode?.({ edits: [] }))
    const w = mount(EditHistoryDialog)
    openEditHistory(request)
    await flushPromises()
    await w.get('button').trigger('click')
    expect(pendingEditHistory.value).toBeNull()
  })
})
