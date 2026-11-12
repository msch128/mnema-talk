// The Show button of a folded message sits inside rows and cards that react
// to clicks and keys themselves: pressing it must reveal the message and
// nothing else.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h } from 'vue'
import BlockedFold from './BlockedFold.vue'
import { useBlocksStore } from '../../stores/blocks'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { setLocale } from '../../i18n'

const BLOCKED = '00000000-0000-4000-8000-0000000000b1'
const msg = { id: '00000000-0000-4000-8000-00000000a001', user_id: BLOCKED }

enableAutoUnmount(afterEach)
beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  useServerFeaturesStore().list = ['blocks']
  useBlocksStore().blocked = [{ user_id: BLOCKED, username: 'troll', display_name: 'Troll', blocked_at: '2026-01-01T00:00:00Z' }]
})

function inARow(onRow: () => void) {
  const Row = defineComponent({
    setup: () => () => h('div', { 'data-testid': 'row', onClick: onRow, onKeydown: onRow }, [
      h(BlockedFold, { msg }, { default: () => h('p', { 'data-testid': 'words' }, 'words') })
    ])
  })
  return mount(Row, { attachTo: document.body })
}

describe('the Show button of a folded message', () => {
  it('keeps Enter and Space from the row around it', async () => {
    const onRow = vi.fn()
    const wrapper = inARow(onRow)
    const show = wrapper.get('[data-testid="blocked-message-show"]')
    await show.trigger('keydown', { key: 'Enter' })
    await show.trigger('keydown', { key: ' ' })
    expect(onRow).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="words"]').exists()).toBe(false)
  })

  it('reveals the message on a click and keeps the click from the row', async () => {
    const onRow = vi.fn()
    const wrapper = inARow(onRow)
    await wrapper.get('[data-testid="blocked-message-show"]').trigger('click')
    expect(onRow).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="words"]').text()).toBe('words')
    expect(wrapper.find('[data-testid="blocked-fold"]').exists()).toBe(false)
  })
})
