import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import MessageRow from './MessageRow.vue'
import { useAuthStore } from '../stores/auth'
import { clearAllDrafts, setDraft, threadDraftKey } from '../lib/drafts'
import { setLocale } from '../i18n'
import { messageFixture, userFixture } from '../test-fixtures.fixture'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  useAuthStore().user = userFixture({ id: 'me' })
})
afterEach(() => clearAllDrafts())

describe('thread entry point', () => {
  it('marks a thread with an unsent reply, also before it has any replies', async () => {
    const msg = messageFixture({ id: 'm1', reply_count: 0 })
    const row = mount(MessageRow, { props: { msg } })
    expect(row.find('[data-testid="thread-draft"]').exists()).toBe(false)
    expect(row.text()).not.toContain('Open thread')

    setDraft('me', threadDraftKey('m1'), { text: 'later', reply: null })
    await nextTick()
    expect(row.get('[data-testid="thread-draft"]').text()).toBe('Draft')
    expect(row.text()).toContain('Open thread')

    await row.setProps({ msg: messageFixture({ id: 'm1', reply_count: 2 }) })
    expect(row.text()).toContain('2 replies')
    expect(row.find('[data-testid="thread-draft"]').exists()).toBe(true)
  })
})
