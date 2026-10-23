import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EphemeralNotes from './EphemeralNotes.vue'
import { useChatStore } from '../stores/chat'

beforeEach(() => setActivePinia(createPinia()))

describe('EphemeralNotes', () => {
  it('formats the private answer of a bot like its normal messages', () => {
    const chat = useChatStore()
    chat.ephemeral = [{ id: 1, channel_id: 'ch1', bot_name: 'Helper', bot_avatar_url: null, content: 'Hello **world** and ||a secret||' } as never]
    const w = mount(EphemeralNotes, { props: { channelId: 'ch1' }, global: { directives: { tooltip: () => undefined } } })
    expect(w.find('strong').text()).toBe('world')
    expect(w.find('[data-testid="ephemeral-notes"]').text()).not.toContain('**')
  })

  it('shows only the notes of its channel', () => {
    const chat = useChatStore()
    chat.ephemeral = [{ id: 1, channel_id: 'other', bot_name: 'Helper', bot_avatar_url: null, content: 'x' } as never]
    const w = mount(EphemeralNotes, { props: { channelId: 'ch1' } })
    expect(w.find('[data-testid="ephemeral-notes"]').exists()).toBe(false)
  })
})
