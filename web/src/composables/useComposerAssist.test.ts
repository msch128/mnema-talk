import { userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { defineComponent, ref, h, nextTick } from 'vue'
import { useComposerAssist } from './useComposerAssist'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useEmojiStore } from '../stores/emoji'
import { usePermissionsStore } from '../stores/permissions'
import { channelFixture } from '../test-fixtures.fixture'

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
  useAuthStore().user = userFixture({ id: 'me', username: 'hera' })
  useChatStore().members = [
    userFixture({ id: 'me', username: 'hera', display_name: 'Hera' }),
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
    await type(w, 'hello world')
    w.find('textarea').element.setSelectionRange(5, 5)
    await w.vm.assist.insertText(' 👋')
    expect(w.vm.text).toBe('hello 👋 world')
    w.unmount()
  })
})

describe('role suggestions in the composer', () => {
  const role = (id: string, name: string, extra: Record<string, unknown> = {}) => ({
    id, name, color: 0, position: 1, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null, ...extra
  })

  it('offers mentionable roles, and every role to someone who may mention everyone', async () => {
    const permissions = usePermissionsStore()
    permissions.roles = [
      role('r1', 'Mods', { mentionable: true }), role('r2', 'Staff'),
      role('r3', 'Bots', { mentionable: true, managed: true }), role('r4', '@everyone', { is_everyone: true, mentionable: true })
    ]
    const channel = channelFixture({ id: 'c1' })
    useChatStore().categories = [{ id: 'cat', name: 'x', sort_order: 0, created_at: '2026-01-01T00:00:00Z', channels: [channel] }]
    useChatStore().activeChannel = channel
    permissions.info = { owner: false, permissions: 0, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [{ channel_id: 'c1', permissions: 2 }] }
    const w = mount(Harness, { attachTo: document.body })
    await type(w, '@')
    expect(w.vm.assist.suggestions.value.filter(s => s.role).map(s => s.username)).toEqual(['Mods'])
    permissions.info = { owner: false, permissions: 0, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [{ channel_id: 'c1', permissions: 2 | 64 }] }
    await type(w, '@')
    expect(w.vm.assist.suggestions.value.filter(s => s.role).map(s => s.username)).toEqual(['Mods', 'Staff'])
    w.unmount()
  })

  it('offers custom emoji after ":" and inserts the chosen :name:', async () => {
    useEmojiStore().items = [{ id: 'e1', name: 'party_parrot', animated: true, url: '/a' }, { id: 'e2', name: 'blob', animated: false, url: '/b' }]
    const w = mount(Harness, { attachTo: document.body })
    await type(w, 'hello :pa')
    expect(w.vm.assist.open.value).toBe(true)
    expect(w.vm.assist.suggestions.value[0]?.emoji?.name).toBe('party_parrot')
    await w.vm.assist.pick(w.vm.assist.suggestions.value[0])
    expect(w.vm.text).toBe('hello :party_parrot: ')
    expect(w.vm.assist.open.value).toBe(false)
    await type(w, ':pa')
    w.vm.assist.close()
    expect(w.vm.assist.open.value).toBe(false)
    w.unmount()
  })

  it('suggests standard emoji by shortcode, custom ones first, and inserts the glyph', async () => {
    useEmojiStore().items = [{ id: 'e1', name: 'smirk_cat_party', animated: false, url: '/a' }]
    const w = mount(Harness, { attachTo: document.body })
    await type(w, 'hi :smi')
    await vi.waitFor(() => expect(w.vm.assist.suggestions.value.length).toBeGreaterThan(1))
    const list = w.vm.assist.suggestions.value
    expect(list[0]?.emoji?.name).toBe('smirk_cat_party')
    const smile = list.find(s => s.username === 'smile')
    expect(smile?.standard).toBe('😄')
    expect(list.length).toBeLessThanOrEqual(8)

    w.vm.assist.active.value = list.indexOf(smile!)
    const key = (k: string) => new KeyboardEvent('keydown', { key: k, cancelable: true })
    expect(w.vm.assist.onKeydown(key('Enter'))).toBe(true)
    await nextTick()
    await nextTick()
    expect(w.vm.text).toBe('hi 😄 ')
    expect(w.vm.assist.open.value).toBe(false)
    w.unmount()
  })

  it('hides a standard emoji whose shortcode a custom emoji already uses, and closes on Escape', async () => {
    useEmojiStore().items = [{ id: 'e1', name: 'smile', animated: false, url: '/a' }]
    const w = mount(Harness, { attachTo: document.body })
    await type(w, ':smile')
    await vi.waitFor(() => expect(w.vm.assist.suggestions.value.length).toBeGreaterThan(0))
    expect(w.vm.assist.suggestions.value.filter(s => s.username === 'smile')).toHaveLength(1)
    expect(w.vm.assist.onKeydown(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }))).toBe(true)
    expect(w.vm.assist.open.value).toBe(false)
    w.unmount()
  })
})
