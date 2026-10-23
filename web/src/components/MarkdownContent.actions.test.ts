import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import MarkdownContent from './MarkdownContent.vue'
import { pendingConfirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'

enableAutoUnmount(afterEach)
beforeEach(() => {
  setActivePinia(createPinia())
  pendingConfirm.value = null
})
afterEach(() => vi.restoreAllMocks())

const mountContent = (content: string) => mount(MarkdownContent, { props: { content, previews: false }, attachTo: document.body })

describe('links whose text does not name their destination', () => {
  function spyOpen() {
    return vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
  }

  it('open only after the reader confirmed where they lead', async () => {
    const opened = spyOpen()
    const w = mountContent('[Click here](https://target.example/path)')
    const click = new MouseEvent('click', { bubbles: true, cancelable: true })
    w.find('a').element.dispatchEvent(click)
    expect(click.defaultPrevented).toBe(true)
    await flushPromises()
    expect(pendingConfirm.value?.body).toContain('target.example')
    expect(pendingConfirm.value?.excerpt).toBe('https://target.example/path')
    expect(opened).not.toHaveBeenCalled()
    pendingConfirm.value?.resolve(true)
    await flushPromises()
    expect(opened).toHaveBeenCalledOnce()
  })

  it('stay closed when the reader declines', async () => {
    const opened = spyOpen()
    const w = mountContent('[Click here](https://target.example)')
    await w.find('a').trigger('click')
    pendingConfirm.value?.resolve(false)
    await flushPromises()
    expect(opened).not.toHaveBeenCalled()
  })

  it('are also held back when opened with the middle button, but other buttons are left alone', async () => {
    const w = mountContent('[Click here](https://target.example)')
    const middle = new MouseEvent('auxclick', { bubbles: true, cancelable: true, button: 1 })
    w.find('a').element.dispatchEvent(middle)
    expect(middle.defaultPrevented).toBe(true)
    pendingConfirm.value = null
    const right = new MouseEvent('auxclick', { bubbles: true, cancelable: true, button: 2 })
    w.find('a').element.dispatchEvent(right)
    expect(right.defaultPrevented).toBe(false)
    expect(pendingConfirm.value).toBeNull()
  })

  it('are left to the browser when the text names the destination or the link is typed plainly', async () => {
    const w = mountContent('[example.com](https://example.com/x) https://example.org <https://example.net>')
    for (const a of w.findAll('a')) {
      const click = new MouseEvent('click', { bubbles: true, cancelable: true })
      a.element.dispatchEvent(click)
      expect(click.defaultPrevented).toBe(false)
    }
    expect(pendingConfirm.value).toBeNull()
  })
})

describe('code block copy button', () => {
  it('copies the code and says so', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const w = mountContent('```js\nlet a = 1\nlet b = "<x>"\n```')
    await w.find('[data-md-copy]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('let a = 1\nlet b = "<x>"')
    expect(useToastStore().toasts.at(-1)?.type).toBe('success')
    vi.unstubAllGlobals()
  })

  it('reports a failed copy', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
    const w = mountContent('```\ncode\n```')
    await w.find('[data-md-copy]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.type).toBe('error')
    vi.unstubAllGlobals()
  })

  it('does nothing for a button without a code block', async () => {
    const writeText = vi.fn()
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const w = mountContent('text')
    const orphan = document.createElement('button')
    orphan.setAttribute('data-md-copy', '')
    w.find('.markdown-body').element.append(orphan)
    orphan.click()
    await flushPromises()
    expect(writeText).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})

describe('previews prop', () => {
  it('suppresses preview cards where the text is only an example', () => {
    const stub = { props: ['url'], template: '<a class="preview">{{ url }}</a>' }
    const on = mount(MarkdownContent, { props: { content: 'https://example.org/a' }, global: { stubs: { LinkPreviewCard: stub } } })
    const off = mount(MarkdownContent, { props: { content: 'https://example.org/a', previews: false }, global: { stubs: { LinkPreviewCard: stub } } })
    expect(on.findAll('.preview')).toHaveLength(1)
    expect(off.findAll('.preview')).toHaveLength(0)
  })
})
