// The dry runs of auto-delete: what a setting would remove of what exists
// now, before the settings ask for confirmation (doc/timed-messages.md).
import { api } from '../../shared/lib/api'
import { decodeChatAutoDeletePreview, type ChatAutoDeletePreview } from '../../types/rest'

/** What a channel's setting (the body of PUT .../auto-delete) would remove. */
export function previewChannelAutoDelete(channelId: string, json: Record<string, unknown>): Promise<ChatAutoDeletePreview> {
  return api(`/api/admin/channels/${channelId}/auto-delete/preview`, { method: 'POST', json, decode: decodeChatAutoDeletePreview })
}

/** What the community default would remove across the channels that follow it. */
export function previewDefaultAutoDelete(json: { hours: number; scope: string }): Promise<ChatAutoDeletePreview> {
  return api('/api/admin/message-settings/auto-delete/preview', { method: 'POST', json, decode: decodeChatAutoDeletePreview })
}
