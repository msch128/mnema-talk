import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ThreadRootCard from './ThreadRootCard.vue'
import { useMessageActions } from '../composables/useMessageActions'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'
import { attachmentFixture, channelFixture, messageFixture, userFixture } from '../test-fixtures.fixture'
import type { Message } from '../types/domain'

let w: VueWrapper
let actions: ReturnType<typeof useMessageActions>

function mountCard(extra: Partial<Message> = {}, canReact = true) {
  const root = messageFixture({ id: 'root', channel_id: 'ch1', user_id: 'me', content: 'First message', ...extra })
  actions = useMessageActions()
  w = mount(ThreadRootCard, { props: { root, actions, canReact }, attachTo: document.body })
  return root
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: 'me' })
  useChatStore().activeChannel = channelFixture({ id: 'ch1', type: 'forum' })
})
afterEach(() => {
  w?.unmount()
  document.body.innerHTML = ''
})

describe('ThreadRootCard', () => {
  it('shows the first message and replies to it', async () => {
    const root = mountCard()
    expect(w.text()).toContain('First message')
    await w.get('div.absolute.right-2 button:last-child').trigger('click')
    expect(w.emitted('reply')).toEqual([[root]])
  })

  it('edits an own first message: pencil, editor, save and cancel', async () => {
    const edit = vi.spyOn(useChatStore(), 'editMessage').mockResolvedValue(null as never)
    mountCard()
    await w.get('[data-testid="root-edit"]').trigger('click')
    expect(actions.editingId.value).toBe('root')
    expect(w.find('[data-testid="root-edit"]').exists()).toBe(false)
    actions.editText.value = 'Better first message'
    const buttons = w.findAll('button')
    await buttons.find(b => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(edit).toHaveBeenCalledWith('ch1', 'root', 'Better first message')

    actions.startEdit(messageFixture({ id: 'root', content: 'x' }))
    await w.vm.$nextTick()
    await w.findAll('button').find(b => b.text() === 'Cancel')!.trigger('click')
    expect(actions.editingId.value).toBeNull()
    expect(w.find('[data-testid="root-edit"]').exists()).toBe(true)
  })

  it('offers no edit for someone else’s post or a poll', () => {
    mountCard({ user_id: 'other' })
    expect(w.find('[data-testid="root-edit"]').exists()).toBe(false)
    w.unmount()
    mountCard({ poll: { id: 'poll1', options: [], total_votes: 0, ended: false, multiple: false } as never })
    expect(w.find('[data-testid="root-edit"]').exists()).toBe(false)
  })

  it('shows the edited and pinned marks', async () => {
    mountCard({ is_edited: true, is_pinned: true, attachments: [attachmentFixture({ id: 'i', mime_type: 'image/png', url: '/api/media/i' })] })
    expect(w.text()).toMatch(/edited/i)
    expect(w.find('[data-testid="thread-root-pinned"]').exists()).toBe(true)
    expect(w.find('img').exists()).toBe(true)
  })

  it('forwards key presses of the root', async () => {
    const root = mountCard()
    await w.get('[data-reply-id="root"]').trigger('keydown', { key: 'r' })
    expect(w.emitted('keydown')![0]![1]).toEqual(root)
  })
})
