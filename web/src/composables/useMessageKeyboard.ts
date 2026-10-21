// Keyboard handling on a focused message row, shared by the channel chat and
// the thread panel: arrows move between rows, r replies, e edits (own
// messages), the menu key / Shift+F10 open the message menu, Escape backs out.

function isTyping(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (target.isContentEditable) return true
  return ['INPUT', 'TEXTAREA'].includes(target.tagName)
}

/**
 * @param e        keydown event on the row (currentTarget is the row).
 * @param handlers.rows    () => list of focusable rows in order.
 * @param handlers.reply   () => void
 * @param handlers.edit    () => boolean, false when the message can't be edited.
 * @param handlers.menu    (rowEl) => void, optional.
 * @param handlers.escape  () => boolean, true when Escape closed something.
 */
type KeyboardHandlers = {
  rows?: () => Iterable<HTMLElement>
  reply?: () => void
  edit?: () => boolean | void
  menu?: (row: HTMLElement) => void
  escape?: () => boolean
}

function isMenuShortcut(e: KeyboardEvent): boolean {
  if (e.key === 'ContextMenu') return true
  return e.key === 'F10' && e.shiftKey
}

function handleMenuKey(e: KeyboardEvent, menu?: (row: HTMLElement) => void): boolean {
  if (!menu) return false
  if (!isMenuShortcut(e)) return false
  e.preventDefault()
  if (e.currentTarget instanceof HTMLElement) {
    menu(e.currentTarget)
  }
  return true
}

function isPlainArrow(e: KeyboardEvent): boolean {
  if (e.altKey || e.ctrlKey) return false
  if (e.metaKey || e.shiftKey) return false
  return e.key === 'ArrowUp' || e.key === 'ArrowDown'
}

function moveRowFocus(e: KeyboardEvent, rows?: () => Iterable<HTMLElement>): boolean {
  if (!isPlainArrow(e)) return false
  if (e.target !== e.currentTarget) return false
  e.preventDefault()
  const list = [...(rows?.() || [])]
  const currentIdx = list.findIndex(row => row === e.currentTarget)
  const offset = e.key === 'ArrowUp' ? -1 : 1
  list[currentIdx + offset]?.focus()
  return true
}

function hasModifierCombo(e: KeyboardEvent): boolean {
  if (e.ctrlKey || e.metaKey) return true
  return e.altKey
}

export function handleMessageKeydown(e: KeyboardEvent, handlers: KeyboardHandlers) {
  if (isTyping(e.target)) return
  if (handleMenuKey(e, handlers.menu)) return
  if (moveRowFocus(e, handlers.rows)) return
  if (hasModifierCombo(e)) return
  handleShortcutKey(e, handlers)
}

function handleShortcutKey(e: KeyboardEvent, { reply, edit, escape }: KeyboardHandlers) {
  if (e.key === 'r' || e.key === 'R') {
    e.preventDefault()
    reply?.()
    return
  }
  if (e.key === 'e' || e.key === 'E') {
    if (edit?.() !== false) e.preventDefault()
    return
  }
  if (e.key === 'Escape' && escape?.()) {
    e.preventDefault()
  }
}
