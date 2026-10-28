import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ChannelWebhooksDialog from './ChannelWebhooksDialog.vue'
import { i18nPlugin } from '../i18n'

enableAutoUnmount(afterEach)

vi.mock('./IncomingWebhooksPanel.vue', () => ({ default: { template: '<div data-testid="incoming" />' } }))
vi.mock('./OutgoingWebhooksPanel.vue', () => ({ default: { template: '<div data-testid="outgoing" />' } }))

const open = () => mount(ChannelWebhooksDialog, { props: { channel: { id: 'c1', name: 'general' } }, global: { plugins: [i18nPlugin] }, attachTo: document.body })

describe('ChannelWebhooksDialog tabs', () => {
  it('links tabs and panel and keeps only the selected tab in the tab order', () => {
    const w = open()
    const tabs = document.body.querySelectorAll('[role="tab"]')
    expect([...tabs].map(t => t.getAttribute('tabindex'))).toEqual(['0', '-1'])
    const panel = document.body.querySelector('[role="tabpanel"]')!
    expect(panel.getAttribute('aria-labelledby')).toBe(tabs[0]!.id)
    expect(tabs[0]!.getAttribute('aria-controls')).toBe(panel.id)
    w.unmount()
  })

  it('moves between tabs with the arrow keys', async () => {
    const w = open()
    const tabs = document.body.querySelectorAll<HTMLElement>('[role="tab"]')
    tabs[0]!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await nextTick(); await nextTick()
    expect(tabs[1]!.getAttribute('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(tabs[1])
    expect(document.body.querySelector('[data-testid="outgoing"]')).not.toBeNull()
    w.unmount()
  })
})
