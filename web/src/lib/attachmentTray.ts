// Rules of the attachment tray: what a message may carry, and what a file
// added to the tray becomes. The server enforces the same limits.

/** Files per message. */
export const MAX_ATTACHMENTS = 10
/** Bytes per message; a single larger file the operator allows still goes alone. */
export const MAX_TOTAL_BYTES = 100 * 1024 * 1024
/** The server deletes an unsent staged file after six hours; the tray uploads it again after five. */
export const STAGED_TTL_MS = 5 * 60 * 60 * 1000
/** Files uploading at the same time. */
export const PARALLEL_UPLOADS = 3

export type TrayKind = 'image' | 'video' | 'file'
export type TrayStatus = 'queued' | 'uploading' | 'ready' | 'error'

export interface TrayItem {
  /** Local id, stable while the file sits in the tray. */
  key: string
  file: File
  /** The channel the file is uploaded to. */
  channelId: string
  kind: TrayKind
  /** Object URL of the file itself, for the thumbnail and the viewer. */
  previewUrl: string | null
  status: TrayStatus
  /** 0..1 */
  progress: number
  /** The server's id once the file is staged. */
  attachmentId: string | null
  /** When the file finished staging (ms since the epoch). */
  stagedAt: number | null
  error: string | null
}

export const kindOf = (file: File): TrayKind => (file.type.startsWith('image/') ? 'image' : file.type.startsWith('video/') ? 'video' : 'file')

export interface AddPlan {
  accept: File[]
  /** Files that did not fit the 10-file limit. */
  overCount: File[]
  /** Files above the per-file size limit. */
  tooLarge: File[]
  /** Files that would push the message above the total size limit. */
  overTotal: File[]
}

/** Splits files offered to a tray that holds `held` files weighing `heldBytes`. */
export function planAdd(held: number, heldBytes: number, files: readonly File[], maxFileMb: number | null): AddPlan {
  const plan: AddPlan = { accept: [], overCount: [], tooLarge: [], overTotal: [] }
  let count = held
  let bytes = heldBytes
  for (const file of files) {
    if (maxFileMb !== null && file.size > maxFileMb * 1024 * 1024) plan.tooLarge.push(file)
    else if (count >= MAX_ATTACHMENTS) plan.overCount.push(file)
    else if (bytes + file.size > Math.max(MAX_TOTAL_BYTES, file.size) && count > 0) plan.overTotal.push(file)
    else {
      plan.accept.push(file)
      count++
      bytes += file.size
    }
  }
  return plan
}
