import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import MessageRow from './MessageRow.vue'
import { useAuthStore } from '../stores/auth'
import { useSavedStore } from '../stores/saved'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import * as actions from '../lib/savedActions'
import { setLocale, t } from '../i18n'
import { messageFixture, userFixture } from '../test-fixtures.fixture'

// The list a saved_updated event reloads.
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: () => Promise.resolve({ items: [], has_more: false })
}))

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  useAuthStore().user = userFixture({ id: 'me' })
  useServerFeaturesStore().list = ['saved']
})

const mountRow = (props: Record<string, unknown> = {}) => mount(MessageRow, {
  props: { msg: messageFixture({ id: 'm1' }), ...props },
  global: { directives: { tooltip: () => undefined } }
})

describe('the saved mark of a message', () => {
  it('stays off a message that is not saved, but the hover bar offers to save it', () => {
    const row = mountRow()
    expect(row.find('[data-testid="saved-mark"]').exists()).toBe(false)
    const toggle = row.get('[data-testid="save-toggle"]')
    expect(toggle.attributes('aria-label')).toBe(t('saved.save'))
    expect(toggle.attributes('aria-pressed')).toBe('false')
  })

  it('marks a saved message for good, and a click on the mark or the bar button takes it off', async () => {
    const toggle = vi.spyOn(actions, 'toggleSavedMessage').mockResolvedValue()
    const saved = useSavedStore()
    saved.onUpdated({ message_id: 'm1', saved: true })
    const row = mountRow()
    const mark = row.get('[data-testid="saved-mark"]')
    expect(mark.attributes('aria-label')).toBe(t('saved.markLabel'))
    expect(row.get('[data-testid="save-toggle"]').attributes('aria-label')).toBe(t('saved.removeFromSaved'))
    expect(row.get('[data-testid="save-toggle"]').attributes('aria-pressed')).toBe('true')
    await mark.trigger('click')
    await row.get('[data-testid="save-toggle"]').trigger('click')
    expect(toggle).toHaveBeenCalledTimes(2)
    expect(toggle).toHaveBeenCalledWith(expect.objectContaining({ id: 'm1' }))
  })

  it('marks a follow-up message that has no header line too', () => {
    useSavedStore().onUpdated({ message_id: 'm1', saved: true })
    const row = mountRow({ grouped: true })
    expect(row.get('[data-testid="saved-mark"]').text()).toBe(t('saved.markShort'))
  })

  it('shows nothing on a server without saved messages', () => {
    useServerFeaturesStore().list = []
    useSavedStore().onUpdated({ message_id: 'm1', saved: true })
    const row = mountRow()
    expect(row.find('[data-testid="saved-mark"]').exists()).toBe(false)
    expect(row.find('[data-testid="save-toggle"]').exists()).toBe(false)
  })
})
