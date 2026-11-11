import { describe, it, expect, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useButtonMenu } from './useButtonMenu'

function setup() {
  setActivePinia(createPinia())
  const m = useButtonMenu(() => [])
  m.button.value = document.createElement('button')
  vi.spyOn(m.menu, 'show').mockImplementation(async () => { m.menu.state.open = true })
  return m
}
const click = (detail: number) => new MouseEvent('click', { detail })

describe('useButtonMenu', () => {
  it('closes with a click on the button while open, and opens again with the next', () => {
    const m = setup()
    m.toggle(click(1))
    expect(m.menu.state.open).toBe(true)
    m.onPointerDown()
    m.menu.state.open = false // the outside dismiss on pointer down
    m.toggle(click(1))
    expect(m.menu.state.open).toBe(false)
    m.toggle(click(1))
    expect(m.menu.state.open).toBe(true)
  })

  it('does not let a press released elsewhere swallow the next keyboard activation', () => {
    const m = setup()
    m.toggle(click(1))
    m.onPointerDown() // pressed while open, released off the button: no click follows
    m.menu.state.open = false
    m.toggle(click(0)) // Enter on the focused button
    expect(m.menu.state.open).toBe(true)
  })
})
