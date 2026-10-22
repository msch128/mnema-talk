import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import MarkdownHelp from './MarkdownHelp.vue'

enableAutoUnmount(afterEach)
beforeEach(() => setActivePinia(createPinia()))

const mountHelp = (props = {}) => mount(MarkdownHelp, { props, attachTo: document.body })

describe('MarkdownHelp', () => {
  it('shows what to type next to how it looks, rendered by the message renderer', async () => {
    const w = mountHelp()
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    await w.find('[data-testid="markdown-help-button"]').trigger('click')
    const dialog = w.find('[role="dialog"]')
    expect(dialog.exists()).toBe(true)
    expect(dialog.find('code').text()).toBe('**bold**')
    expect(dialog.find('strong').text()).toBe('bold')
    expect(dialog.find('.md-spoiler').exists()).toBe(true)
    expect(dialog.find('h1').text()).toBe('Heading')
    expect(dialog.find('.md-codeblock').exists()).toBe(true)
  })

  it('never starts link previews from its examples', async () => {
    const w = mountHelp()
    await w.find('[data-testid="markdown-help-button"]').trigger('click')
    expect(w.findComponent({ name: 'LinkPreviewCard' }).exists()).toBe(false)
  })

  it('closes with Escape, the close button and a click elsewhere, and hands focus back', async () => {
    const w = mountHelp()
    const open = () => w.find('[data-testid="markdown-help-button"]').trigger('click')
    await open()
    await w.find('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    expect(document.activeElement).toBe(w.find('[data-testid="markdown-help-button"]').element)
    await open()
    await w.find('[role="dialog"] button').trigger('click')
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    await open()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.find('[role="dialog"]').exists()).toBe(false)
  })

  it('stays open for clicks inside', async () => {
    const w = mountHelp()
    await w.find('[data-testid="markdown-help-button"]').trigger('click')
    w.find('[role="dialog"]').element.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.find('[role="dialog"]').exists()).toBe(true)
  })

  it('can be disabled with the composer', () => {
    expect(mountHelp({ disabled: true }).find('[data-testid="markdown-help-button"]').attributes('disabled')).toBeDefined()
  })
})
