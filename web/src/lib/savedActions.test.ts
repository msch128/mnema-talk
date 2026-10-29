import type { ApiOptions } from './api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { toggleSavedMessage } from './savedActions'
import { pendingPanel } from './savedPanel'
import { useSavedStore } from '../stores/saved'
import { useToastStore } from '../stores/toast'
import { setLocale, t } from '../i18n'
import { fixtureId, FIXTURE_TIMESTAMP, messageFixture } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('./api', async importOriginal => ({
  ...(await importOriginal<typeof import('./api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const msg = messageFixture({ id: fixtureId(30) })
const saved = { message_id: msg.id, note: '', saved_at: FIXTURE_TIMESTAMP, available: true, message: msg }

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  apiMock.mockReset()
  pendingPanel.value = null
})

describe('saving a message', () => {
  it('says where it went and offers to show it in the list', async () => {
    apiMock.mockResolvedValue(saved)
    await toggleSavedMessage(msg)
    const toast = useToastStore().toasts.at(-1)
    expect(toast?.text).toBe(t('saved.added'))
    expect(toast?.action?.label).toBe(t('saved.view'))
    expect(useSavedStore().isSaved(msg.id)).toBe(true)
    expect(useSavedStore().hintPending).toBe(true)
    await toast?.action?.onClick()
    expect(pendingPanel.value).toEqual({ tab: 'saved', messageId: msg.id })
  })

  it('takes a saved message off again, without a View action', async () => {
    apiMock.mockResolvedValue(saved)
    await toggleSavedMessage(msg)
    apiMock.mockResolvedValue(null)
    await toggleSavedMessage(msg)
    const toast = useToastStore().toasts.at(-1)
    expect(toast?.text).toBe(t('saved.removed'))
    expect(toast?.action).toBeNull()
    expect(useSavedStore().isSaved(msg.id)).toBe(false)
  })

  it('tells the member when saving fails and shows no hint', async () => {
    apiMock.mockRejectedValue(null)
    await toggleSavedMessage(msg)
    expect(useToastStore().toasts.at(-1)?.text).toBe(t('saved.saveFailed'))
    expect(useSavedStore().hintPending).toBe(false)
  })
})
