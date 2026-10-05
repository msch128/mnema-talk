import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { defineComponent, ref, h, nextTick } from 'vue'
import { useComposerAssist } from './useComposerAssist'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'

const Harness = defineComponent({
  setup(_, { expose }) {
    const el = ref(null)
    const text = ref('')
    const assist = useComposerAssist(el, text)
    expose({ assist, text, el })
    return () => h('textarea', {
      ref: el,
      value: text.value,
      onInput: e => { text.value = e.target.value; assist.onInput() },
      onKeydown: e => assist.onKeydown(e)
    })
  }
})

beforeEach(() => {
  setActivePinia(createPinia())
  useAuthStore().user = { id: 'me', username: 'herzog' }
  useChatStore().members = [
    { id: 'me', username: 'herzog', display_name: 'Herzog' },
    { id: 'u2', username: 'max', display_name: 'Max' },
    { id: 'u3', username: 'moritz', display_name: 'Moritz' }
  ]
})

async function type(w, value) {
  const ta = w.find('textarea')
  ta.element.value = value
  ta.element.setSelectionRange(value.length, value.length)
  await ta.trigger('input')
}

describe('useComposerAssist', () => {
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
