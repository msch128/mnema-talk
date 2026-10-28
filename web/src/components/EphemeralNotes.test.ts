import { beforeEach, describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
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

  it('keeps a live region and moves focus on when a note is dismissed', async () => {
    const chat = useChatStore()
    chat.ephemeral = [
      { id: 1, channel_id: 'ch1', bot_name: 'Helper', bot_avatar_url: null, content: 'one' },
      { id: 2, channel_id: 'ch1', bot_name: 'Helper', bot_avatar_url: null, content: 'two' }
    ] as never
    const composer = document.createElement('textarea')
    composer.setAttribute('data-composer-input', '')
    document.body.appendChild(composer)
    const w = mount(EphemeralNotes, { props: { channelId: 'ch1' }, attachTo: document.body, global: { directives: { tooltip: () => undefined } } })
    expect(w.attributes('role')).toBe('status')
    await w.findAll('[data-ephemeral-dismiss]')[0]!.trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(w.get('[data-ephemeral-dismiss]').element)
    await w.get('[data-ephemeral-dismiss]').trigger('click')
    await flushPromises()
    expect(w.attributes('role')).toBe('status')
    expect(document.activeElement).toBe(composer)
    w.unmount()
    composer.remove()
  })

  it('shows only the notes of its channel', () => {
    const chat = useChatStore()
    chat.ephemeral = [{ id: 1, channel_id: 'other', bot_name: 'Helper', bot_avatar_url: null, content: 'x' } as never]
    const w = mount(EphemeralNotes, { props: { channelId: 'ch1' } })
    expect(w.find('[data-testid="ephemeral-notes"]').exists()).toBe(false)
  })
})
