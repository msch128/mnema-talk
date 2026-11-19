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

/**
 * The part of the window a floating panel may use: the visual viewport (what
 * a zoomed or keyboard-covered page really shows), less the safe-area insets
 * of a notch or home indicator (the padding #app takes for them).
 */
export function visibleArea(): { top: number; bottom: number; width: number } {
  const vv = typeof window !== 'undefined' ? window.visualViewport : null
  const app = typeof document !== 'undefined' ? document.getElementById('app') : null
  const pad = app ? getComputedStyle(app) : null
  const insetTop = parseFloat(pad?.paddingTop ?? '') || 0
  const insetBottom = parseFloat(pad?.paddingBottom ?? '') || 0
  const top = (vv?.offsetTop ?? 0) + insetTop
  const bottom = (vv ? vv.offsetTop + vv.height : window.innerHeight) - insetBottom
  return { top, bottom, width: vv?.width ?? window.innerWidth }
}

/**
 * The point a panel grows out of and shrinks back to (its transform-origin,
 * in px of the panel): where it meets its anchor. Below the anchor that is
 * the panel's top edge, above it the bottom edge, across at the anchor's
 * middle (a pointer's own position).
 */
export function growOrigin(anchor: AnchorBox, at: { x: number; y: number }, size: { width: number; height: number }): string {
  const x = clamp((anchor.left + anchor.right) / 2 - at.x, 0, size.width)
  const y = at.y >= anchor.bottom - 1 ? 0 : at.y + size.height <= anchor.top + 1 ? size.height : clamp(anchor.top - at.y, 0, size.height)
  return `${Math.round(x)}px ${Math.round(y)}px`
}

/** Where a menu is attached: the given target, else the element in full screen (only it is drawn there), else the page. */
export function menuHost(target?: string | HTMLElement): string | HTMLElement {
  if (target) return target
  const full = typeof document === 'undefined' ? null : document.fullscreenElement
  return full instanceof HTMLElement && full !== document.documentElement ? full : 'body'
}
