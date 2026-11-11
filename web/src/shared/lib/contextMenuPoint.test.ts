import { afterEach, describe, expect, it, vi } from 'vitest'
import { menuPointFromEvent } from './contextMenuPoint'

afterEach(() => { vi.restoreAllMocks() })

describe('menu and confirmation edge cases', () => {
  it('uses partial mouse coordinates, keyboard anchors, and safe empty fallbacks', () => {
    expect(menuPointFromEvent({ clientX: 4 })).toEqual({ x: 4, y: 0, anchor: null })
    expect(menuPointFromEvent({ clientY: 6 })).toEqual({ x: 0, y: 6, anchor: null })
    const element = document.createElement('button')
    vi.spyOn(element, 'getBoundingClientRect').mockReturnValue(new DOMRect(12, 13, 30, 20))
    expect(menuPointFromEvent({ clientX: 0, clientY: 0, currentTarget: element })).toEqual({ x: 12, y: 33, anchor: element })
    expect(menuPointFromEvent({ currentTarget: window, target: element })).toEqual({ x: 12, y: 33, anchor: element })
    for (const event of [null, undefined, {}, { target: window }, { currentTarget: null, target: null }]) expect(menuPointFromEvent(event)).toEqual({ x: 0, y: 0, anchor: null })
  })
})
