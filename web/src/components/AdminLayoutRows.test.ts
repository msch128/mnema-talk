import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { Trash2 } from '@lucide/vue'
import AdminChannelRow from './AdminChannelRow.vue'
import AdminCategoryHeader from './AdminCategoryHeader.vue'
import RowActionsMenu from '../shared/components/RowActionsMenu.vue'
import { useCompactLayout } from '../shared/composables/useCompactLayout'
import { setLocale } from '../i18n'
import type { Category, Channel } from '../types/domain'

// On a wide screen the layout editor rows show their actions as icon buttons.
beforeEach(() => {
  setLocale('en')
  useCompactLayout().isCompact.value = false
})
afterEach(() => { document.body.innerHTML = '' })

const channel = (over: Partial<Channel> = {}) => ({ id: 'c1', name: 'general', type: 'text', ...over }) as Channel
const category = { id: 'k1', name: 'Lounge', channels: [{ id: 'c1' }, { id: 'c2' }] } as unknown as Category

describe('layout editor channel row', () => {
  it('moves, renames and deletes through its icon buttons', async () => {
    const w = mount(AdminChannelRow, { props: { channel: channel(), index: 1, count: 3 } })
    await w.get('[data-testid="move-up-channel"]').trigger('click')
    await w.get('[data-testid="move-down-channel"]').trigger('click')
    await w.get('[data-testid="rename-channel"]').trigger('click')
    await w.get('[data-testid="delete-channel"]').trigger('click')
    expect(w.emitted('move')).toEqual([[-1], [1]])
    expect(w.emitted('rename')).toHaveLength(1)
    expect(w.emitted('delete')).toHaveLength(1)
  })

  it('cannot move the first channel up or the last one down', () => {
    const first = mount(AdminChannelRow, { props: { channel: channel(), index: 0, count: 2 } })
    expect(first.get('[data-testid="move-up-channel"]').attributes('disabled')).toBeDefined()
    const last = mount(AdminChannelRow, { props: { channel: channel(), index: 1, count: 2 } })
    expect(last.get('[data-testid="move-down-channel"]').attributes('disabled')).toBeDefined()
  })

  it('passes its drag events up without bubbling them to the category', async () => {
    const w = mount(AdminChannelRow, { props: { channel: channel(), index: 0, count: 1 } })
    const row = w.get('[data-testid="channel-item-c1"]')
    await row.trigger('dragstart')
    await row.trigger('dragover')
    await row.trigger('drop')
    await row.trigger('dragend')
    expect(w.emitted('dragstart')).toHaveLength(1)
    expect(w.emitted('drop')).toHaveLength(1)
    expect(w.emitted('dragend')).toHaveLength(1)
  })

  it('shows the topic of a text channel and a different icon for a forum', () => {
    expect(mount(AdminChannelRow, { props: { channel: channel({ topic: 'chat here' }), index: 0, count: 1 } }).text()).toContain('(chat here)')
    const voice = mount(AdminChannelRow, { props: { channel: channel({ type: 'voice' }), index: 0, count: 1 } })
    const forum = mount(AdminChannelRow, { props: { channel: channel({ type: 'forum' } as Partial<Channel>), index: 0, count: 1 } })
    expect(voice.html()).not.toBe(forum.html())
  })
})

describe('layout editor category header', () => {
  it('adds a channel, moves, renames and deletes through its icon buttons', async () => {
    const w = mount(AdminCategoryHeader, { props: { category, index: 1, count: 3 } })
    expect(w.text()).toContain('(2)')
    await w.get('[data-testid="add-channel-category"]').trigger('click')
    await w.get('[data-testid="move-up-category"]').trigger('click')
    await w.get('[data-testid="move-down-category"]').trigger('click')
    await w.get('[data-testid="rename-category"]').trigger('click')
    await w.get('[data-testid="delete-category"]').trigger('click')
    expect(w.emitted('add-channel')).toHaveLength(1)
    expect(w.emitted('move')).toEqual([[-1], [1]])
    expect(w.emitted('rename')).toHaveLength(1)
    expect(w.emitted('delete')).toHaveLength(1)
  })

  it('cannot move the first category up or the last one down, and counts none when empty', () => {
    const empty = { id: 'k2', name: 'Empty' } as unknown as Category
    const first = mount(AdminCategoryHeader, { props: { category: empty, index: 0, count: 1 } })
    expect(first.get('[data-testid="move-up-category"]').attributes('disabled')).toBeDefined()
    expect(first.get('[data-testid="move-down-category"]').attributes('disabled')).toBeDefined()
    expect(first.text()).toContain('(0)')
  })
})

describe('compact layout menus', () => {
  const run = async (w: ReturnType<typeof mount>, label: RegExp) => {
    await w.get('[data-testid="row-actions"]').trigger('click')
    await w.vm.$nextTick()
    await w.vm.$nextTick()
    ;[...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(el => label.test(el.textContent ?? ''))?.click()
  }

  it('runs every channel action from the menu', async () => {
    useCompactLayout().isCompact.value = true
    for (const [re, ev] of [[/up/, 'move'], [/down/, 'move'], [/Edit channel/, 'rename']] as const) {
      const w = mount(AdminChannelRow, { props: { channel: channel(), index: 1, count: 3 }, attachTo: document.body })
      await run(w, re)
      expect(w.emitted(ev), String(re)).toHaveLength(1)
      document.body.innerHTML = ''
    }
  })

  it('runs every category action from the menu', async () => {
    useCompactLayout().isCompact.value = true
    for (const [re, ev] of [[/Add channel/, 'add-channel'], [/up/, 'move'], [/down/, 'move'], [/Delete category/, 'delete']] as const) {
      const w = mount(AdminCategoryHeader, { props: { category, index: 1, count: 3 }, attachTo: document.body })
      await run(w, re)
      expect(w.emitted(ev), String(re)).toHaveLength(1)
      document.body.innerHTML = ''
    }
  })

  it('shows no button when a row has no actions', () => {
    const w = mount(RowActionsMenu, { props: { items: [], label: 'Actions' } })
    expect(w.find('[data-testid="row-actions"]').exists()).toBe(false)
  })

  it('marks the button expanded while the menu is open', async () => {
    const w = mount(RowActionsMenu, { props: { items: [{ id: 'x', label: 'Remove', icon: Trash2, action: () => {} }], label: 'Actions' }, attachTo: document.body })
    const btn = w.get('[data-testid="row-actions"]')
    expect(btn.attributes('aria-expanded')).toBe('false')
    await btn.trigger('pointerdown')
    await btn.trigger('click')
    await w.vm.$nextTick()
    expect(btn.attributes('aria-expanded')).toBe('true')
  })
})
