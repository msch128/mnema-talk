import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import { ApiError } from '../lib/api'
import SelfUpdateDialog from './SelfUpdateDialog.vue'
import BaseDialog from './BaseDialog.vue'
import { userFixture } from '../test-fixtures.fixture'
import { button } from './adminTestkit.fixture'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (original) => ({
  ...(await original<typeof import('../lib/api')>()),
  api: (path: string, options?: ApiOptions) => apiMock(path, options),
}))
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
describe('self update confirmation', () => {
  it('guards empty and concurrent submits, clears credentials, and emits result', async () => {
    let finish: ((value: unknown) => void) | undefined
    apiMock.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    const w = keep(
      mount(SelfUpdateDialog, {
        props: { currentVersion: '0.4.0', targetVersion: '0.5.0' },
      }),
    )
    await w.find('form').trigger('submit')
    expect(apiMock).not.toHaveBeenCalled()
    await w.find('input').setValue('synthetic password')
    await w.find('form').trigger('submit')
    await w.find('form').trigger('submit')
    expect(apiMock).toHaveBeenCalledTimes(1)
    expect(
      w.find('[data-testid="self-update-confirm"]').attributes('disabled'),
    ).toBeDefined()
    if (!finish) throw new Error('Missing request resolver')
    finish({
      status: 'started',
      from_version: '0.4.0',
      target_version: '0.5.0',
    })
    await flushPromises()
    expect(w.emitted('started')).toEqual([
      [{ status: 'started', from_version: '0.4.0', target_version: '0.5.0' }],
    ])
    expect(w.find('input').element).toHaveProperty('value', '')
    await button(w, 'Cancel').trigger('click')
    w.findComponent(BaseDialog).vm.$emit('close')
    expect(w.emitted('close')).toHaveLength(2)
  })
  it.each([
    new ApiError(429, 'RATE_LIMITED', 'limited'),
    new Error('denied'),
    null,
  ])('reports a rejected update and clears credentials', async (reason) => {
    apiMock.mockRejectedValue(reason)
    const w = keep(
      mount(SelfUpdateDialog, {
        props: {
          currentVersion: '0.4.0',
          targetVersion: '0.5.0',
          releaseUrl: 'https://example.com/releases',
        },
      }),
    )
    await w.find('input').setValue('synthetic password')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[role="alert"]').text()).not.toBe('')
    expect(w.find('input').element).toHaveProperty('value', '')
    expect(w.find('a').attributes('rel')).toContain('noopener')
  })
})
