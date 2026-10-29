// The one media viewer of the app: any component opens it with the files to
// show (and, for a message, who sent it and where), a single component mounted
// in App.vue renders it.
import { reactive, readonly } from 'vue'
import type { Message } from '../types/domain'
import { shownName } from '../lib/deletedUser'

export interface ViewerItem {
  src: string
  name: string
  sizeBytes?: number
  mimeType?: string
  width?: number
  height?: number
}

export interface ViewerMessage {
  id: string
  channelId: string
  channelName?: string
  senderName: string
  senderAvatarUrl?: string
  sentAt: string
}

export interface ViewerRequest {
  items: ViewerItem[]
  index: number
  message?: ViewerMessage
}

const state = reactive<{ open: boolean; items: ViewerItem[]; index: number; message: ViewerMessage | null }>({
  open: false,
  items: [],
  index: 0,
  message: null
})

export const viewerState = readonly(state)

export function openViewer({ items, index, message }: ViewerRequest): void {
  if (!items.length) return
  state.items = items.map(item => ({ ...item }))
  state.index = Math.min(Math.max(0, index), items.length - 1)
  state.message = message ? { ...message } : null
  state.open = true
}

export function closeViewer(): void {
  state.open = false
  state.items = []
  state.index = 0
  state.message = null
}

export function setViewerIndex(index: number): void {
  if (index >= 0 && index < state.items.length) state.index = index
}

/** The viewer's message details for a chat message. */
export function viewerMessageFrom(msg: Message, channelName?: string): ViewerMessage {
  return {
    id: msg.id,
    channelId: msg.channel_id,
    senderName: shownName(msg),
    sentAt: msg.created_at,
    ...(channelName ? { channelName } : {}),
    ...(msg.avatar_url ? { senderAvatarUrl: msg.avatar_url } : {})
  }
}
