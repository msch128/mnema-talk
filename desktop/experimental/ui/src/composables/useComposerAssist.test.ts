import { userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { defineComponent, ref, h, nextTick } from 'vue'
import { useComposerAssist } from './useComposerAssist'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'

const Harness = defineComponent({
  setup() {
    const el = ref<HTMLTextAreaElement | null>(null)
    const text = ref('')
    const assist = useComposerAssist(el, text)
    return { assist, text, el }
  },
  render() { return h('textarea', {
      ref: 'el',
      value: this.text,
      onInput: (e: Event) => { if (e.target instanceof HTMLTextAreaElement) this.text = e.target.value; this.assist.onInput() },
      onKeydown: (e: KeyboardEvent) => this.assist.onKeydown(e)
    }) }
})

beforeEach(() => {
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: 'me', username: 'herzog' })
  useChatStore().members = [
    userFixture({ id: 'me', username: 'herzog', display_name: 'Herzog' }),
    userFixture({ id: 'u2', username: 'max', display_name: 'Max' }),
    userFixture({ id: 'u3', username: 'moritz', display_name: 'Moritz' })
  ]
})

async function type(w: ReturnType<typeof mount<typeof Harness>>, value: string) {
  const ta = w.find('textarea')
  ta.element.value = value
  ta.element.setSelectionRange(value.length, value.length)
  await ta.trigger('input')
}

describe('useComposerAssist', () => {
  it('wraps arrow navigation, consumes Tab, and leaves ordinary and shifted Enter keys', async () => {
    const w = mount(Harness, { attachTo: document.body })
    await type(w, '@m')
    const a = w.vm.assist
    const key = (key: string, shiftKey = false) => new KeyboardEvent('keydown', { key, shiftKey, cancelable: true })
    expect(a.onKeydown(key('ArrowUp'))).toBe(true)
    expect(a.active.value).toBe(1)
    expect(a.onKeydown(key('ArrowDown'))).toBe(true)
    expect(a.active.value).toBe(0)
    expect(a.onKeydown(key('x'))).toBe(false)
    expect(a.onKeydown(key('Enter', true))).toBe(false)
    expect(a.onKeydown(key('Tab'))).toBe(true)
    await nextTick()
    expect(w.vm.text).toBe('@max ')
    expect(a.onKeydown(key('ArrowDown'))).toBe(false)
    w.unmount()
  })

  it('retains dismissal until a new query and handles absent textarea or suggestion', async () => {
    const el = ref<HTMLTextAreaElement | null>(null)
    const text = ref('@m')
    const a = useComposerAssist(el, text)
    a.onInput()
    a.close()
    await a.pick(undefined)
    await a.insertText('!')
    expect(text.value).toBe('@m!')
    el.value = document.createElement('textarea')
    text.value = '@m'
    el.value.setSelectionRange(2, 2)
    el.value.value = text.value
    el.value.setSelectionRange(2, 2)
    a.onInput()
    await a.pick(undefined)
    expect(a.open.value).toBe(true)
    a.close()
    a.onInput()
    expect(a.open.value).toBe(false)
    text.value = 'plain'
    el.value.value = text.value
    el.value.setSelectionRange(5, 5)
    a.onInput()
    text.value = '@m'
    el.value.value = text.value
    el.value.setSelectionRange(2, 2)
    a.onInput()
    expect(a.open.value).toBe(true)
    a.active.value = 100
    a.onInput()
    expect(a.active.value).toBe(0)
    el.value = null
    await a.pick(a.suggestions.value[0])
    expect(text.value).toBe('@m')
    text.value = ''
    await a.insertText('hi')
    expect(text.value).toBe('hi')
    useAuthStore().user = null
    expect(a.suggestions.value).toHaveLength(2)
    el.value = document.createElement('textarea')
    Object.defineProperties(el.value, { selectionStart: { value: null }, selectionEnd: { value: null } })
    await a.insertText('!')
    expect(text.value).toBe('hi!')
  })
  it('suggests members (never yourself) and groups for "@"', async () => {
    const w = mount(Harness, { attachTo: document.body })
    await type(w, 'hey @m')
    expect(w.vm.assist.suggestions.value.map(s => s.username)).toEqual(['max', 'moritz'])
    await type(w, 'hey @')
    expect(w.vm.assist.suggestions.value.map(s => s.username)).toEqual(['max', 'moritz', 'here', 'all'])
    w.unmount()
  })

  it('picks with the keyboard and inserts "@username "', async () => {
    const w = mount(Harness, { attachTo: document.body })
    await type(w, 'hey @mo')
    await w.find('textarea').trigger('keydown', { key: 'Enter' })
    await nextTick()
    expect(w.vm.text).toBe('hey @moritz ')
    expect(w.vm.assist.open.value).toBe(false)
    w.unmount()
  })

  it('Escape closes the list without sending', async () => {
    const w = mount(Harness, { attachTo: document.body })
    await type(w, '@ma')
    await w.find('textarea').trigger('keydown', { key: 'Escape' })
    expect(w.vm.assist.open.value).toBe(false)
    expect(w.vm.text).toBe('@ma')
    w.unmount()
  })

  it('inserts emoji at the caret', async () => {
    const w = mount(Harness, { attachTo: document.body })
    await type(w, 'hallo welt')
    w.find('textarea').element.setSelectionRange(5, 5)
    await w.vm.assist.insertText(' 👋')
    expect(w.vm.text).toBe('hallo 👋 welt')
    w.unmount()
  })
})
