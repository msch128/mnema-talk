import type { ServerEventPayloads } from '../../types/events'
import { TYPING_EXPIRY_MS, TYPING_THROTTLE_MS } from './types'
import type { Cx } from './state'
import { sendWSEvent } from './socket'

export function sendTyping(cx: Cx, channelId?: string) {
  const { cs } = cx
  const id = channelId || cs.activeChannel.value?.id
  if (!id) return

  // Per channel: typing in a thread of another channel right after this one
  // still announces itself there.
  const now = Date.now()
  if (now - (cs.lastTypingSentAt.get(id) ?? -Infinity) < TYPING_THROTTLE_MS) return
  cs.lastTypingSentAt.set(id, now)
  sendWSEvent(cx, 'typing', { channel_id: id })
}

export function handleTypingEvent(cx: Cx, payload: ServerEventPayloads['typing'] & { username?: string; display_name?: string }) {
  const { cs, authStore } = cx
  if (!payload?.channel_id || !payload?.user_id) return
  const { channel_id, user_id } = payload
  if (user_id === authStore.user?.id) return

  const member = cs.members.value.find(m => m.id === user_id)
  const typer = {
    user_id,
    username: payload.username || member?.username || '',
    display_name: payload.display_name || member?.display_name || member?.username || ''
  }

  const current = cs.typingByChannel.value[channel_id] || []
  const filtered = current.filter(u => u.user_id !== user_id)
  cs.typingByChannel.value = {
    ...cs.typingByChannel.value,
    [channel_id]: [...filtered, typer]
  }

  const key = `${channel_id}:${user_id}`
  if (cs.typingTimers.has(key)) {
    clearTimeout(cs.typingTimers.get(key))
  }
  const timer = setTimeout(() => {
    cs.typingTimers.delete(key)
    const list = cs.typingByChannel.value[channel_id] || []
    cs.typingByChannel.value = {
      ...cs.typingByChannel.value,
      [channel_id]: list.filter(u => u.user_id !== user_id)
    }
  }, TYPING_EXPIRY_MS)
  cs.typingTimers.set(key, timer)
}

export function clearTypingForUser(cx: Cx, channelId: string, userId: string) {
  const { cs } = cx
  if (!channelId || !userId) return
  const key = `${channelId}:${userId}`
  if (cs.typingTimers.has(key)) {
    clearTimeout(cs.typingTimers.get(key))
    cs.typingTimers.delete(key)
  }
  const current = cs.typingByChannel.value[channelId]
  if (current) {
    const list = current.filter(u => u.user_id !== userId)
    cs.typingByChannel.value = {
      ...cs.typingByChannel.value,
      [channelId]: list
    }
  }
}

