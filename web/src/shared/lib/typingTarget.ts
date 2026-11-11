// Whether a key event is typing into a field. Web components such as the emoji
// picker keep their search input in a shadow root, and events leaving it are
// retargeted to the host element, so the check reads the composed path.

const FIELD = 'input, textarea, select, [contenteditable]:not([contenteditable="false"])'

function originOf(e: Event): EventTarget | null {
  const path = typeof e.composedPath === 'function' ? e.composedPath() : []
  return path[0] ?? e.target
}

export function isTypingTarget(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  if (el.isContentEditable) return true
  return !!el.closest?.(FIELD)
}

export function isTypingEvent(e: Event): boolean {
  return isTypingTarget(originOf(e)) || isTypingTarget(e.target)
}
