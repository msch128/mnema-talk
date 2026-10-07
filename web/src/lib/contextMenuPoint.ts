// Where a context menu should open for a contextmenu / keyboard event.
// Mouse: the pointer. Keyboard (Shift+F10, Menu key): the focused element's
// box, because those events carry no usable coordinates.
export interface MenuTrigger {
  clientX?: number
  clientY?: number
  currentTarget?: EventTarget | null
  target?: EventTarget | null
  preventDefault?: () => void
  stopPropagation?: () => void
}

export interface MenuPoint { x: number; y: number; anchor: Element | null }
export function menuPointFromEvent(e: MenuTrigger | null | undefined): MenuPoint {
  if (e && (e.clientX || e.clientY)) return { x: e.clientX ?? 0, y: e.clientY ?? 0, anchor: null }
  const el = e?.currentTarget instanceof Element ? e.currentTarget : e?.target
  if (el instanceof Element) {
    const r = el.getBoundingClientRect()
    return { x: r.left, y: r.bottom, anchor: el }
  }
  return { x: 0, y: 0, anchor: null }
}
