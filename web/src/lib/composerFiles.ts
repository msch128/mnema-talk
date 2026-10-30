// What a paste or drop carries: files to stage in the attachment tray, or text.

/** Whether a drag carries files (as opposed to selected text or a link). */
export const dragHasFiles = (dt: DataTransfer | null) => !!dt && Array.from(dt.types ?? []).includes('Files')

export const filesOf = (dt: DataTransfer | null): File[] => Array.from(dt?.files ?? [])

/**
 * The files of a paste, or none when it is text. Pasted office cells and web
 * selections carry a rendered picture next to their HTML or text; those stay
 * text. A copied picture has no text, a copied file in the Finder or Explorer
 * has at most its name.
 */
export function pastedFiles(dt: DataTransfer | null): File[] {
  const files = filesOf(dt)
  if (!files.length || !dt) return []
  if (dt.getData('text/html')) return dt.getData('text/plain').trim() ? [] : files
  return files
}

/** The height of a growing textarea: its content, up to a share of the window. */
export function growHeight(scrollHeight: number, viewportHeight: number, share = 0.45) {
  const max = Math.max(0, Math.round(viewportHeight * share))
  return { height: Math.min(scrollHeight, max), scrolls: scrollHeight > max }
}
