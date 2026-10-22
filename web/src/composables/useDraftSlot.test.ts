import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h, nextTick, ref } from 'vue'
import { useDraftSlot } from './useDraftSlot'
import { useAuthStore } from '../stores/auth'
import { clearAllDrafts, DRAFT_KEY_PREFIX, getDraft, hasDraft, type DraftReply } from '../lib/drafts'
import { userFixture } from '../test-fixtures.fixture'

const slot = ref<string | null>('c:one')
const text = ref('')
const reply = ref<DraftReply | null>(null)

const Harness = defineComponent({
  setup() {
    useDraftSlot(() => slot.value, text, reply)
    return () => h('div')
  }
})

enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.useFakeTimers()
  localStorage.clear()
  clearAllDrafts()
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: 'me' })
  slot.value = 'c:one'
  text.value = ''
  reply.value = null
})
afterEach(() => vi.useRealTimers())

async function typeText(value: string) {
  text.value = value
  await nextTick()
  vi.advanceTimersByTime(500)
}

describe('draft per composer slot', () => {
  it('restores each slot\'s text and reply when switching back and forth', async () => {
    mount(Harness)
    await typeText('first')
    reply.value = { id: 'm1', display_name: 'Max' }
    await nextTick()
    slot.value = 'c:two'
    expect(text.value).toBe('')
    expect(reply.value).toBeNull()
    await typeText('second')
    slot.value = 'c:one'
    expect(text.value).toBe('first')
    expect(reply.value).toEqual({ id: 'm1', display_name: 'Max' })
    slot.value = 'c:two'
    expect(text.value).toBe('second')
  })

  it('saves the old slot at once when the switch comes straight after typing', async () => {
    mount(Harness)
    text.value = 'quick'
    slot.value = 'c:two'
    expect(getDraft('me', 'c:one')?.text).toBe('quick')
    expect(text.value).toBe('')
  })

  it('restores the draft after a reload', () => {
    localStorage.setItem(`${DRAFT_KEY_PREFIX}me`, JSON.stringify({ 'c:one': { text: 'from before', reply: { id: 'm2' }, at: 1 } }))
    mount(Harness)
    expect(text.value).toBe('from before')
    expect(reply.value).toEqual({ id: 'm2' })
  })

  it('forgets the draft once the composer is cleared by a send', async () => {
    mount(Harness)
    await typeText('hello')
    expect(hasDraft('me', 'c:one')).toBe(true)
    await typeText('')
    expect(hasDraft('me', 'c:one')).toBe(false)
  })

  it('saves on unmount and never after sign-out', async () => {
    const view = mount(Harness)
    text.value = 'unsaved'
    await nextTick()
    view.unmount()
    expect(getDraft('me', 'c:one')?.text).toBe('unsaved')

    const second = mount(Harness)
    text.value = 'after logout'
    await nextTick()
    useAuthStore().user = null
    clearAllDrafts()
    vi.advanceTimersByTime(500)
    second.unmount()
    expect(Object.keys(localStorage)).toEqual([])
  })

  it('keeps nothing without a slot or without an account', async () => {
    slot.value = null
    mount(Harness)
    await typeText('nowhere')
    expect(Object.keys(localStorage)).toEqual([])
    slot.value = 'c:one'
    useAuthStore().user = null
    await typeText('anonymous')
    expect(Object.keys(localStorage)).toEqual([])
  })
})
