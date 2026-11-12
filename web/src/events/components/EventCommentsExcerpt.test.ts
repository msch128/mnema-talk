import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EventComments from './EventComments.vue'
import { useAuthStore } from '../../stores/auth'
import { pendingConfirm } from '../../shared/lib/confirm'
import * as api from '../lib/eventCommentsApi'
import { setLocale } from '../../i18n'
import { commentFixture } from '../../eventRoles.fixture'
import { fixtureId, userFixture } from '../../test-fixtures.fixture'

vi.mock('../lib/eventCommentsApi', () => ({
  fetchComments: vi.fn(), postComment: vi.fn(), editComment: vi.fn(), deleteComment: vi.fn(), setCommentsMuted: vi.fn()
}))
vi.mock('../lib/eventsDialog', () => ({ openEvents: vi.fn() }))

let wrapper: VueWrapper

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.clearAllMocks()
  useAuthStore().user = userFixture({ id: fixtureId(1) })
  pendingConfirm.value = null
})
afterEach(() => wrapper?.unmount())

// C-L3-N4 (S4). The message, forum and reminder dialogs show previewText(...)
// ("||x||" reads "Spoiler"); the comment dialog cuts the raw Markdown source,
// so a spoiler is read out in the confirmation. Expected failure: it turns red
// the day the dialog shows the plain preview.
describe('the comment delete dialog excerpt', () => {
  it.fails('does not show the text of a spoiler or the Markdown marks', async () => {
    const mine = commentFixture(1, { content: 'the **butler** ||did it||' })
    vi.mocked(api.fetchComments).mockResolvedValue({ comments: [mine], has_more: false, muted: false })
    wrapper = mount(EventComments, { props: { eventId: fixtureId(1) } })
    await flushPromises()
    await wrapper.get('[data-testid="comment-delete"]').trigger('click')
    const excerpt = pendingConfirm.value?.excerpt ?? ''
    expect(excerpt).not.toContain('did it')
    expect(excerpt).not.toContain('**')
  })
})
