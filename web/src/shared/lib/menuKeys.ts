// Keyboard helpers of ContextMenu: which row an arrow key moves to, and type-ahead.

export function rowTarget(key: string, cur: number): number | null {
  if (key === 'ArrowDown') return cur < 0 ? 0 : cur + 1
  if (key === 'ArrowUp') return cur < 0 ? -1 : cur - 1
  if (key === 'Home') return 0
  if (key === 'End') return -1
  return null
}

function isPrintableCharKey(e: KeyboardEvent): boolean {
  if (e.ctrlKey || e.metaKey || e.altKey) return false
  if (e.key.length !== 1) return false
  return /\S/.test(e.key)
}

// Type-ahead: next row whose label starts with the typed letter.
export function typeAhead(e: KeyboardEvent, list: HTMLElement[], cur: number) {
  if (!isPrintableCharKey(e)) return
  const ch = e.key.toLowerCase()
  const n = list.length
  for (let k = 1; k <= n; k++) {
    const el = list[(Math.max(cur, -1) + k) % n]
    if (el?.textContent?.trim().toLowerCase().startsWith(ch)) {
      el.focus()
      break
    }
  }
}
