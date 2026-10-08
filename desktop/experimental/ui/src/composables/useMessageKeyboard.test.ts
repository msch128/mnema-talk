import { required } from '../store-test-support.fixture'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { handleMessageKeydown } from './useMessageKeyboard'

function rows(n: number) {
  const list = []
  for (let i = 0; i < n; i++) {
    const el = document.createElement('div')
    el.tabIndex = 0
    document.body.appendChild(el)
    list.push(el)
  }
  return list
}

// Dispatches on `target` while the handler is bound on `row`.
function press(rowValue: HTMLElement | undefined, init: KeyboardEventInit, handlers: Parameters<typeof handleMessageKeydown>[1], targetValue: HTMLElement | undefined = rowValue) {
  const row = required(rowValue)
  const target = required(targetValue)
  const ev = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init })
  const listener = (e: KeyboardEvent) => handleMessageKeydown(e, handlers)
  row.addEventListener('keydown', listener)
  target.dispatchEvent(ev)
  row.removeEventListener('keydown', listener)
  return ev
}

afterEach(() => { document.body.innerHTML = '' })

describe('handleMessageKeydown', () => {
  it('handles absent rows and handlers, uppercase keys and modifier shortcuts', () => {
    const [row] = rows(1)
    expect(press(row, { key: 'ArrowDown' }, {}).defaultPrevented).toBe(true)
    expect(press(row, { key: 'R' }, {}).defaultPrevented).toBe(true)
    expect(press(row, { key: 'E' }, {}).defaultPrevented).toBe(true)
    expect(press(row, { key: 'Escape' }, {}).defaultPrevented).toBe(false)
    expect(press(row, { key: 'R', metaKey: true }, {}).defaultPrevented).toBe(false)
    expect(press(row, { key: 'R', altKey: true }, {}).defaultPrevented).toBe(false)
    const event = new KeyboardEvent('keydown', { key: 'ContextMenu', cancelable: true })
    const menu = vi.fn()
    handleMessageKeydown(event, { menu })
    expect(menu).not.toHaveBeenCalled()
    expect(event.defaultPrevented).toBe(true)
  })
  it('moves focus with the arrow keys', () => {
    const list = rows(3)
    const h = { rows: () => list }
    required(list[1]).focus()
    press(list[1], { key: 'ArrowDown' }, h)
    expect(document.activeElement).toBe(list[2])
    press(list[2], { key: 'ArrowUp' }, h)
    expect(document.activeElement).toBe(list[1])
  })

  it('r replies, e edits only when allowed', () => {
    const [row] = rows(1)
    const reply = vi.fn()
    expect(press(row, { key: 'r' }, { reply }).defaultPrevented).toBe(true)
    expect(reply).toHaveBeenCalled()
    expect(press(row, { key: 'e' }, { edit: () => false }).defaultPrevented).toBe(false)
    expect(press(row, { key: 'e' }, { edit: () => true }).defaultPrevented).toBe(true)
  })

  it('leaves browser shortcuts and typing alone', () => {
    const [row] = rows(1)
    const reply = vi.fn()
    press(row, { key: 'r', ctrlKey: true }, { reply })
    const input = document.createElement('textarea')
    required(row).appendChild(input)
    press(row, { key: 'r' }, { reply }, input)
    expect(reply).not.toHaveBeenCalled()
  })

  it('opens the menu with the menu key or Shift+F10', () => {
    const [row] = rows(1)
    const menu = vi.fn()
    press(row, { key: 'ContextMenu' }, { menu })
    press(row, { key: 'F10', shiftKey: true }, { menu })
    expect(menu).toHaveBeenCalledTimes(2)
    expect(menu).toHaveBeenCalledWith(row)
  })

  it('Escape is only consumed when something closed', () => {
    const [row] = rows(1)
    expect(press(row, { key: 'Escape' }, { escape: () => false }).defaultPrevented).toBe(false)
    expect(press(row, { key: 'Escape' }, { escape: () => true }).defaultPrevented).toBe(true)
  })

  it('leaves unassigned keys on focused message rows to the browser', () => {
    const [row] = rows(1)
    expect(press(row, { key: 'Home' }, {}).defaultPrevented).toBe(false)
  })
})
