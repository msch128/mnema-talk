// Where a context menu should open for a contextmenu / keyboard event.
// Mouse: the pointer. Keyboard (Shift+F10, Menu key): the focused element's
// box, because those events carry no usable coordinates.
export function menuPointFromEvent(e) {
  if (e && (e.clientX || e.clientY)) return { x: e.clientX, y: e.clientY, anchor: null }
  const el = e?.currentTarget instanceof Element ? e.currentTarget : e?.target
  if (el && typeof el.getBoundingClientRect === 'function') {
    const r = el.getBoundingClientRect()
    return { x: r.left, y: r.bottom, anchor: el }
  }
  return { x: 0, y: 0, anchor: null }
}
