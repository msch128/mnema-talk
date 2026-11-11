import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { usePreferenceSave } from './usePreferenceSave'
import { useToastStore } from '../../stores/toast'
import { setLocale } from '../../i18n'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.useFakeTimers()
})
afterEach(() => { vi.useRealTimers() })

function setup() {
  let pref!: ReturnType<typeof usePreferenceSave>
  mount(defineComponent({ setup() { pref = usePreferenceSave(1000); return () => h('div') } }))
  return pref
}

describe('usePreferenceSave', () => {
  it('shows "Saved" for a moment after a change went through', async () => {
    const pref = setup()
    expect(await pref.run(() => Promise.resolve())).toBe(true)
    expect(pref.saved.value).toBe(true)
    vi.advanceTimersByTime(1000)
    expect(pref.saved.value).toBe(false)
    expect(useToastStore().toasts).toHaveLength(0)
  })

  it('reports a failed change with a toast and shows no "Saved"', async () => {
    const pref = setup()
    expect(await pref.run(() => Promise.reject(new Error('refused')))).toBe(false)
    expect(pref.saved.value).toBe(false)
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['refused'])
  })
})
