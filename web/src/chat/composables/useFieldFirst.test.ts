import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { useFieldFirst } from './useFieldFirst'

// The composer's field comes first in the DOM exactly when the stylesheet
// draws it first, and follows the row as it is resized.
describe('useFieldFirst', () => {
  let resize: (() => void) | null = null
  const disconnect = vi.fn()

  // jsdom knows no flex order; the stylesheet's answer is read from data-order.
  beforeEach(() => {
    vi.spyOn(window, 'getComputedStyle').mockImplementation(el => ({ order: (el as HTMLElement).dataset.order ?? '0' }) as CSSStyleDeclaration)
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    resize = null
    disconnect.mockClear()
  })

  function stubResizeObserver() {
    vi.stubGlobal('ResizeObserver', class {
      constructor(callback: () => void) { resize = callback }
      observe() {}
      disconnect() { disconnect() }
    })
  }

  function setup() {
    const row = ref<HTMLElement | null>(null)
    const field = ref<HTMLElement | null>(document.createElement('textarea'))
    const scope = effectScope()
    const fieldFirst = scope.run(() => useFieldFirst(row, field))!
    return { row, field, scope, fieldFirst }
  }

  it('says the field is first while it is drawn first, and changes with the row size', async () => {
    stubResizeObserver()
    const { row, field, scope, fieldFirst } = setup()
    expect(fieldFirst.value).toBe(false)

    field.value!.dataset.order = '-9999'
    row.value = document.createElement('div')
    await nextTick()
    expect(fieldFirst.value).toBe(true)

    field.value!.dataset.order = '0'
    resize?.()
    expect(fieldFirst.value).toBe(false)

    scope.stop()
    expect(disconnect).toHaveBeenCalled()
  })

  it('keeps the tools first without a field or without ResizeObserver', async () => {
    vi.stubGlobal('ResizeObserver', undefined)
    const { row, field, fieldFirst } = setup()
    field.value = null
    row.value = document.createElement('div')
    await nextTick()
    expect(fieldFirst.value).toBe(false)
    row.value = null
    await nextTick()
    expect(fieldFirst.value).toBe(false)
  })
})
