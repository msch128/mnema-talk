import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import TalkParticipants from './TalkParticipants.vue'
import { useChatStore } from '../stores/chat'
import { voiceUserFixture, requireValue } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
let wrapper: ReturnType<typeof mount<typeof TalkParticipants>> | undefined
const alice = voiceUserFixture({ id: 'a', username: 'alice', display_name: 'Alice', joined_at: '2026-01-01T10:00:00Z' })
const bob = voiceUserFixture({ id: 'b', username: 'bob', display_name: '' })
beforeEach(() => { setActivePinia(createPinia()); setLocale('en') })
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = ''; vi.restoreAllMocks() })
describe('TalkParticipants keyboard and dismissal', () => {
  it('focuses first participant, wraps arrow keys and opens selected profile', async () => {
    const profile = vi.spyOn(useChatStore(), 'openUserProfile').mockResolvedValue(undefined)
    wrapper = mount(TalkParticipants, { props: { users: [alice, bob], startedAt: '2026-01-01T10:00:00Z' }, attachTo: document.body })
    const button = wrapper.get<HTMLButtonElement>('[data-testid="talk-participants"]')
    await button.trigger('click'); await nextTick()
    const items = wrapper.findAll<HTMLButtonElement>('[role="menuitem"]')
    const first = requireValue(items[0]); const second = requireValue(items[1])
    expect(document.activeElement).toBe(first.element)
    await first.trigger('keydown', { key: 'ArrowUp' }); expect(document.activeElement).toBe(second.element)
    await second.trigger('keydown', { key: 'ArrowDown' }); expect(document.activeElement).toBe(first.element)
    await first.trigger('keydown', { key: 'Home' }); expect(document.activeElement).toBe(first.element)
    expect(second.text()).toContain('bob')
    await second.trigger('click'); expect(profile).toHaveBeenCalledWith(bob)
    expect(button.attributes('aria-expanded')).toBe('false')
  })
  it('returns focus on toggle/Escape, dismisses outside pointer, handles empty menu', async () => {
    wrapper = mount(TalkParticipants, { props: { users: [] }, attachTo: document.body })
    const button = wrapper.get<HTMLButtonElement>('button')
    await button.trigger('click'); expect(wrapper.find('[role="menu"]').exists()).toBe(true)
    await wrapper.trigger('keydown', { key: 'ArrowDown' }); await wrapper.trigger('keydown', { key: 'ArrowUp' })
    await button.trigger('click'); expect(document.activeElement).toBe(button.element)
    await button.trigger('click'); document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); await nextTick()
    expect(button.attributes('aria-expanded')).toBe('false'); expect(document.activeElement).toBe(button.element)
    await button.trigger('click'); document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true })); await nextTick()
    expect(button.attributes('aria-expanded')).toBe('false')
    const rootElement = wrapper.element
    wrapper.unmount()
    rootElement.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  })
})
