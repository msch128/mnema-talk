// Keeps an absolutely placed card inside the column that clips it (the chat
// or forum area, which hides what runs past its edges). The card keeps its
// anchor when it fits; otherwise it is narrowed to the column and moved right
// until its left edge is inside. Positions go through the CSSOM, which the
// Content-Security-Policy allows (style attributes are blocked).

import { layoutLeft } from './layoutBox'

const GAP = 8

export function keepInsideColumn(card: HTMLElement): void {
  card.style.removeProperty('max-width')
  card.style.removeProperty('translate')
  if (getComputedStyle(card).position !== 'absolute') return
  const column = card.closest('main, section, [data-testid="forum-area"]')
  if (!column) return
  const bounds = column.getBoundingClientRect()
  const room = bounds.width - 2 * GAP
  // Layout sizes: the card may still be scaled down by its opening animation.
  if (card.offsetWidth > room) card.style.setProperty('max-width', `${Math.max(room, 0)}px`)
  const shift = bounds.left + GAP - layoutLeft(card)
  if (shift > 0) card.style.setProperty('translate', `${shift}px 0`)
}
