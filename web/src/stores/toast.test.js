import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useToastStore } from './toast'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
})
afterEach(() => vi.useRealTimers())

describe('toast store', () => {
  it('dismisses info and success toasts after 5 seconds', () => {
    const toasts = useToastStore()
    toasts.info('a')
    toasts.success('b')
    expect(toasts.toasts.map(t => t.type)).toEqual(['info', 'success'])
    vi.advanceTimersByTime(4999)
    expect(toasts.toasts).toHaveLength(2)
    vi.advanceTimersByTime(2)
    expect(toasts.toasts).toHaveLength(0)
  })

  it('keeps errors and toasts with an action until dismissed', () => {
    const toasts = useToastStore()
    const e = toasts.error('boom')
    toasts.info('retry?', { action: { label: 'Erneut versuchen', onClick() {} } })
    vi.advanceTimersByTime(60000)
    expect(toasts.toasts).toHaveLength(2)
    toasts.dismiss(e)
    expect(toasts.toasts).toHaveLength(1)
  })

  it('lets a toast with an action dismiss itself after an explicit duration (undo)', () => {
    const toasts = useToastStore()
    toasts.success('Kanal verschoben', { duration: 6000, action: { label: 'Rückgängig', onClick() {} } })
    vi.advanceTimersByTime(5999)
    expect(toasts.toasts).toHaveLength(1)
    expect(toasts.toasts[0].action.label).toBe('Rückgängig')
    vi.advanceTimersByTime(1)
    expect(toasts.toasts).toHaveLength(0)
  })

  it('stacks at most four toasts and ignores empty text', () => {
    const toasts = useToastStore()
    for (let i = 0; i < 6; i++) toasts.info(`t${i}`)
    expect(toasts.push({ text: '' })).toBeNull()
    expect(toasts.toasts.map(t => t.text)).toEqual(['t2', 't3', 't4', 't5'])
  })
})
