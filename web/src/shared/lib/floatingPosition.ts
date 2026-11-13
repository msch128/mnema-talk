// Where a floating panel (a context menu, the "send later" menu) opens, given
// the thing it belongs to. One rule for all of them: below the anchor and
// aligned to its left edge; if that runs off the window it flips above the
// anchor and/or aligns to the anchor's right edge; what still does not fit is
// kept inside the window with a margin. A pointer position is an anchor
// without size, so a right-click near an edge opens towards the inside.
export interface AnchorBox { left: number; top: number; right: number; bottom: number }

interface PlaceOptions {
  /** Space between the anchor and the panel. */
  gap?: number
  /** Minimum distance to the window edge. */
  pad?: number
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(value, max))
}

function placeVertical(anchor: AnchorBox, height: number, viewHeight: number, gap: number, pad: number): number {
  const below = anchor.bottom + gap
  if (below + height <= viewHeight - pad) return below
  const above = anchor.top - gap - height
  if (above >= pad) return above
  // Neither side has room: the larger one, pushed inside.
  const roomBelow = viewHeight - anchor.bottom
  const roomAbove = anchor.top
  return clamp(roomBelow >= roomAbove ? below : above, pad, Math.max(pad, viewHeight - pad - height))
}

function placeHorizontal(anchor: AnchorBox, width: number, viewWidth: number, pad: number): number {
  const left = anchor.left
  if (left + width <= viewWidth - pad) return left
  return clamp(anchor.right - width, pad, Math.max(pad, viewWidth - pad - width))
}

export function placeNear(
  anchor: AnchorBox,
  size: { width: number; height: number },
  view: { width: number; height: number },
  { gap = 4, pad = 8 }: PlaceOptions = {}
): { x: number; y: number } {
  return {
    x: placeHorizontal(anchor, size.width, view.width, pad),
    y: placeVertical(anchor, size.height, view.height, gap, pad)
  }
}

/** A point (pointer position) as an anchor without size. */
export function pointBox(x: number, y: number): AnchorBox {
  return { left: x, top: y, right: x, bottom: y }
}

/** Plain numbers for an element's box; keeps the state free of DOM objects. */
export function boxOf(rect: { left: number; top: number; right: number; bottom: number }): AnchorBox {
  return { left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom }
}

const PAD = 8

/**
 * Where a submenu goes beside its row: flipped to the left when the right
 * side has no room and the left has; otherwise shifted back into the window
 * (on a narrow screen it then overlaps its parent). `shift` moves it up.
 */
export function submenuOffsets(panel: AnchorBox & { width: number }, row: AnchorBox | null, viewport: { width: number; height: number }): { flip: boolean; shift: number; shiftX: number } {
  let flip = false
  let shiftX = 0
  let shift = 0
  if (panel.right > viewport.width - PAD) {
    if (row && row.left - 6 - panel.width >= PAD) flip = true
    else shiftX = -Math.min(panel.left - PAD, panel.right - (viewport.width - PAD))
  }
  if (panel.bottom > viewport.height - PAD) shift = -Math.min(panel.top - PAD, panel.bottom - (viewport.height - PAD))
  return { flip, shift, shiftX }
}

/** A menu taller than the window (a short window, a large zoom) opens as a sheet from the bottom edge instead. */
export function needsSheet(menuHeight: number, viewportHeight: number): boolean {
  return menuHeight > viewportHeight - 2 * PAD
}
