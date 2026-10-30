import { api } from './api'
import { decodeMediaAttachment, type MediaAttachment } from '../types/domain'

/**
 * Uploads one file for a message that is not sent yet. The file stays the
 * caller's own until a message lists its id in `attachment_ids`; the server
 * deletes it after a few hours otherwise.
 */
export function stageAttachment(
  channelId: string,
  file: File,
  opts: { signal?: AbortSignal; onProgress?: (loaded: number, total: number) => void } = {}
): Promise<MediaAttachment> {
  const form = new FormData()
  form.append('file', file)
  return api(`/api/channels/${channelId}/attachments`, {
    method: 'POST',
    form,
    decode: decodeMediaAttachment,
    ...(opts.signal ? { signal: opts.signal } : {}),
    ...(opts.onProgress ? { onUploadProgress: opts.onProgress } : {})
  })
}

/** Removes a staged file that will not be sent. */
export async function unstageAttachment(channelId: string, attachmentId: string): Promise<void> {
  await api(`/api/channels/${channelId}/attachments/${attachmentId}`, { method: 'DELETE' })
}
