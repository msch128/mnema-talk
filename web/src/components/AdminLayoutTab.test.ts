import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import type { ApiOptions } from '../lib/api'
import AdminLayoutTab from './AdminLayoutTab.vue'
import BaseDialog from './BaseDialog.vue'
import { useChatStore } from '../stores/chat'
import { categoryFixture, channelFixture, fixtureId, userFixture } from '../test-fixtures.fixture'
import { tree, errors, button } from './adminTestkit.fixture'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (original) => ({
  ...(await original<typeof import('../lib/api')>()),
  api: (path: string, options?: ApiOptions) => apiMock(path, options),
}))
const confirmMock = vi.fn<() => Promise<boolean>>()
vi.mock('../lib/confirm', () => ({ confirm: () => confirmMock() }))
// The live operations panel has its own tests and endpoint.
vi.mock('./AdminOpsPanel.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./AdminUploadLimit.vue', () => ({ default: { template: '<div />' } }))

let wrappers: VueWrapper[] = []
function keep<T extends VueWrapper>(w: T): T {
  wrappers.push(w)
  return w
}
beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset().mockResolvedValue(true)
  useAuthStore().user = userFixture({ role: 'admin', username: 'owner' }) // the Owner may do everything
})
afterEach(() => {
  for (const w of wrappers) w.unmount()
  wrappers = []
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
  document.body.innerHTML = ''
})
describe('layout editing recovery and unsaved order', () => {
  async function openLayout() {
    vi.spyOn(useChatStore(), 'fetchChannels').mockResolvedValue(undefined)
    apiMock.mockImplementation(async (path, options) => {
      if (path === '/api/channels') return tree()
      if (path === '/api/admin/categories' && options?.method === 'POST')
        return categoryFixture({ id: fixtureId(99), name: 'New category' })
      if (options?.method === 'PATCH')
        return channelFixture({ name: 'server name', topic: 'server topic' })
      return { ok: true }
    })
    const w = keep(mount(AdminLayoutTab))
    await flushPromises()
    return w
  }
  const cat = (w: VueWrapper, id = 20) =>
    w.find(`[data-testid="category-item-${fixtureId(id)}"]`)
  const ch = (w: VueWrapper, id = 21) =>
    w.find(`[data-testid="channel-item-${fixtureId(id)}"]`)
  async function dirty(w: VueWrapper) {
    await cat(w).find('[data-testid="move-down-channel"]').trigger('click')
  }
  it('reorders categories and uncategorized channels in both directions and exercises native drag events', async () => {
    const w = await openLayout()
    await cat(w).find('[data-testid="move-down-category"]').trigger('click')
    await cat(w).find('[data-testid="move-up-category"]').trigger('click')
    await ch(w, 10).find('[data-testid="move-down-channel"]').trigger('click')
    await ch(w, 10).find('[data-testid="move-up-channel"]').trigger('click')
    await ch(w).find('[data-testid="move-down-channel"]').trigger('click')
    await ch(w).find('[data-testid="move-up-channel"]').trigger('click')
    const transfer = { effectAllowed: '' }
    await cat(w).trigger('dragstart', { dataTransfer: transfer })
    expect(transfer.effectAllowed).toBe('move')
    await cat(w, 30).trigger('drop')
    await cat(w, 30).trigger('dragend')
    await cat(w).trigger('dragover')
    await ch(w, 10).trigger('dragstart', { dataTransfer: transfer })
    await ch(w, 11).trigger('dragover')
    await ch(w, 11).trigger('drop')
    await ch(w, 11).trigger('dragend')
    await ch(w).trigger('dragstart', { dataTransfer: transfer })
    await ch(w, 22).trigger('dragover')
    await ch(w).trigger('dragend')
    await cat(w, 30).find('div.border-l-2').trigger('dragover')
    await cat(w, 30).find('div.border-l-2').trigger('drop')
    await ch(w, 11).trigger('dragover')
    await ch(w, 11).trigger('dragstart')
    await ch(w, 11).trigger('dragend')
    await w
      .findAll('button')
      .find((b) => b.text() === 'Save layout')!
      .trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(
      '/api/admin/layout',
      expect.objectContaining({ method: 'PUT' }),
    )
  })
  it('renames categories with unsaved order, validates empty submissions, and permits both cancellation controls', async () => {
    const w = await openLayout()
    await dirty(w)
    await cat(w).find('[data-testid="rename-category"]').trigger('click')
    await w.find('form input').setValue('  ')
    await w.find('form').trigger('submit')
    expect(
      apiMock.mock.calls.some(([, opts]) => opts?.method === 'PATCH'),
    ).toBe(false)
    await w.find('form input').setValue(' Renamed ')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(cat(w).text()).toContain('Renamed')
    expect(w.text()).toContain('Unsaved changes')
    await cat(w).find('[data-testid="rename-category"]').trigger('click')
    await button(w, 'Cancel').trigger('click')
    expect(w.find('form').exists()).toBe(false)
    await cat(w).find('[data-testid="rename-category"]').trigger('click')
    w.findComponent(BaseDialog).vm.$emit('close')
    await nextTick()
    expect(w.find('form').exists()).toBe(false)
  })
  it('creates categories through enter and click, preserves unsaved state, and reports failure without clearing input', async () => {
    const w = await openLayout()
    const input = w.find('input')
    await input.setValue(' ')
    await input.trigger('keydown.enter')
    expect(apiMock.mock.calls.some(([, opts]) => opts?.method === 'POST')).toBe(
      false,
    )
    await dirty(w)
    await input.setValue('New category')
    await input.trigger('keydown.enter')
    await flushPromises()
    expect(cat(w, 99).text()).toContain('New category')
    expect(input.element).toHaveProperty('value', '')
    await input.setValue('Retry')
    apiMock.mockRejectedValueOnce(new Error('create denied'))
    w.find('input').element.parentElement!.querySelector('button')!.click()
    await flushPromises()
    expect(errors()).toContain('create denied')
    expect(input.element).toHaveProperty('value', 'Retry')
  })
  it.each(['category', 'channel'] as const)(
    'keeps a failed %s edit open and supports cancellation',
    async (kind) => {
      const w = await openLayout()
      const item = kind === 'category' ? cat(w) : ch(w, 10)
      await item.find(`[data-testid="rename-${kind}"]`).trigger('click')
      if (kind === 'channel') {
        await w.find('form input').setValue(' ')
        await w.find('form').trigger('submit')
        expect(
          apiMock.mock.calls.some(([, opts]) => opts?.method === 'PATCH'),
        ).toBe(false)
      }
      await w.find('form input').setValue('renamed')
      apiMock.mockRejectedValueOnce(new Error('edit denied'))
      await w.find('form').trigger('submit')
      await flushPromises()
      expect(errors()).toContain('edit denied')
      expect(w.find('form').exists()).toBe(true)
      await button(w, 'Cancel').trigger('click')
      expect(w.find('form').exists()).toBe(false)
      await item.find(`[data-testid="rename-${kind}"]`).trigger('click')
      w.findComponent(BaseDialog).vm.$emit('close')
      await nextTick()
      expect(w.find('form').exists()).toBe(false)
    },
  )
  it.each(['category', 'channel'] as const)(
    'cancels %s deletion and reports mutation failure',
    async (kind) => {
      const w = await openLayout()
      const item = kind === 'category' ? cat(w) : ch(w, 10)
      confirmMock.mockResolvedValueOnce(false)
      apiMock.mockClear()
      await item.find(`[data-testid="delete-${kind}"]`).trigger('click')
      await flushPromises()
      expect(apiMock).not.toHaveBeenCalled()
      apiMock.mockRejectedValueOnce(new Error('delete denied'))
      await item.find(`[data-testid="delete-${kind}"]`).trigger('click')
      await flushPromises()
      expect(errors()).toContain('delete denied')
      expect(item.exists()).toBe(true)
    },
  )
  it('deletes an uncategorized channel while preserving unsaved order', async () => {
    const w = await openLayout()
    await dirty(w)
    await ch(w, 10).find('[data-testid="delete-channel"]').trigger('click')
    await flushPromises()
    expect(ch(w, 10).exists()).toBe(false)
    expect(w.text()).toContain('Unsaved changes')
  })
  it('reports a save failure and preserves the unsaved layout', async () => {
    const w = await openLayout()
    await dirty(w)
    apiMock.mockRejectedValueOnce(new Error('save denied'))
    await w.find('[data-testid="save-layout-button"]').trigger('click')
    await flushPromises()
    expect(errors()).toContain('save denied')
    expect(w.text()).toContain('Unsaved changes')
    expect(
      w.find('[data-testid="save-layout-button"]').attributes('disabled'),
    ).toBeUndefined()
  })
  it('retains a local edit when its follow-up channel reload fails', async () => {
    const w = await openLayout()
    await ch(w).find('[data-testid="rename-channel"]').trigger('click')
    await w.find('form input').setValue('local name')
    apiMock
      .mockResolvedValueOnce(channelFixture({ name: 'local name' }))
      .mockRejectedValueOnce(new Error('reload denied'))
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(ch(w).text()).toContain('local name')
    expect(errors()).toContain('reload denied')
  })
  it('preserves a reorder begun while a post-edit reload is in flight', async () => {
    const w = await openLayout()
    await cat(w).find('[data-testid="rename-category"]').trigger('click')
    await w.find('form input').setValue('new name')
    let resolveReload: ((value: unknown) => void) | undefined
    apiMock.mockResolvedValueOnce({ ok: true }).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveReload = resolve
        }),
    )
    await w.find('form').trigger('submit')
    await flushPromises()
    await dirty(w)
    if (!resolveReload) throw new Error('Missing reload resolver')
    resolveReload(tree())
    await flushPromises()
    expect(cat(w).text()).toContain('new name')
    expect(w.text()).toContain('Unsaved changes')
  })
  it('handles initial loading failures', async () => {
    apiMock.mockRejectedValue(new Error('layout offline'))
    const w = keep(mount(AdminLayoutTab))
    await flushPromises()
    expect(errors()).toContain('layout offline')
    expect(w.findAll('[data-testid^="category-item-"]')).toHaveLength(0)
  })
})
describe('layout defensive stale action handling', () => {
  it('opens empty names for editing and invokes the channel topic update handler', async () => {
    const data = tree()
    data.categories[0]!.name = ''
    data.categories[0]!.channels[0]!.name = ''
    data.categories[0]!.channels[0]!.topic = ''
    apiMock.mockResolvedValue(data)
    const w = keep(mount(AdminLayoutTab))
    await flushPromises()
    await w.find('[data-testid="rename-category"]').trigger('click')
    expect(w.find('form input').element).toHaveProperty('value', '')
    await button(w, 'Cancel').trigger('click')
    await w
      .find(
        `[data-testid="channel-item-${fixtureId(21)}"] [data-testid="rename-channel"]`,
      )
      .trigger('click')
    expect(w.find('form input').element).toHaveProperty('value', '')
    await w.findAll('form input')[1]!.setValue('new topic')
    expect(w.findAll('form input')[1]!.element).toHaveProperty(
      'value',
      'new topic',
    )
    await button(w, 'Cancel').trigger('click')
  })
  it.each(['category', 'channel'] as const)(
    'safely ignores a successful %s rename after concurrent removal',
    async (kind) => {
      vi.spyOn(useChatStore(), 'fetchChannels').mockResolvedValue(undefined)
      apiMock.mockResolvedValue(tree())
      const w = keep(mount(AdminLayoutTab))
      await flushPromises()
      await w
        .find(
          `[data-testid="category-item-${fixtureId(20)}"] [data-testid="move-down-channel"]`,
        )
        .trigger('click')
      const item = w.find(
        `[data-testid="${kind}-item-${fixtureId(kind === 'category' ? 20 : 21)}"]`,
      )
      await item.find(`[data-testid="rename-${kind}"]`).trigger('click')
      await w.find('form input').setValue('Renamed')
      let complete: ((value: unknown) => void) | undefined
      apiMock.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            complete = resolve
          }),
      )
      await w.find('form').trigger('submit')
      await item.find(`[data-testid="delete-${kind}"]`).trigger('click')
      await flushPromises()
      if (!complete) throw new Error('Missing rename resolver')
      complete(channelFixture({ name: 'Renamed' }))
      await flushPromises()
      expect(
        w
          .find(
            `[data-testid="${kind}-item-${fixtureId(kind === 'category' ? 20 : 21)}"]`,
          )
          .exists(),
      ).toBe(false)
      expect(errors()).toEqual([])
    },
  )
})

describe('layout stale arrow handler', () => {
  it('ignores an arrow from a channel removed by another action', async () => {
    vi.spyOn(useChatStore(), 'fetchChannels').mockResolvedValue(undefined)
    apiMock.mockResolvedValue(tree())
    const w = keep(mount(AdminLayoutTab))
    await flushPromises()
    const row = w.find(`[data-testid="channel-item-${fixtureId(22)}"]`)
    const arrow = row.find('[data-testid="move-up-channel"]')
    await arrow.trigger('click')
    const downArrow = row.find('[data-testid="move-down-channel"]')
    await row.find('[data-testid="delete-channel"]').trigger('click')
    await flushPromises()
    await downArrow.trigger('click')
    expect(w.find(`[data-testid="channel-item-${fixtureId(22)}"]`).exists()).toBe(false)
    expect(errors()).toEqual([])
  })
})
