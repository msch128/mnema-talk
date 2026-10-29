// What a paste or drop may attach to a message, decided before the upload path
// sees it. The file button accepts images and videos, so these do too.

export interface Attachable {
  file: File | null
  /** Files beyond the first: a message carries one. */
  extra: number
  /** Files that are neither an image nor a video. */
  rejected: number
}

export const isMedia = (file: File) => file.type.startsWith('image/') || file.type.startsWith('video/')

export function pickAttachable(files: readonly File[]): Attachable {
  const media = files.filter(isMedia)
  return { file: media[0] ?? null, extra: Math.max(0, media.length - 1), rejected: files.length - media.length }
}

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
