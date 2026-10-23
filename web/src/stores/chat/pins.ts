import type { Message } from '../../types/domain'
import type { ServerEventPayloads } from '../../types/events'
import { isChatMessage } from '../../types/rest'
import { arrayDecoder } from '../../types/validation'
import { api } from '../../lib/api'
import { MAX_PIN_NOTICES } from './types'
import type { Cx } from './state'

const decodeMessages = arrayDecoder('messages', isChatMessage)

/** The pinned messages of a channel, the most recently pinned first. */
export function fetchPins(_cx: Cx, channelId: string): Promise<Message[]> {
  return api(`/api/channels/${channelId}/pins`, { decode: decodeMessages })
}

function signalPins(cx: Cx, channelId: string) {
  const previous = cx.cs.pinsSignal.value
  cx.cs.pinsSignal.value = { channelId, seq: (previous?.seq ?? 0) + 1 }
}

/** A message was pinned or unpinned: an open pinned list refetches, a new pin is noted under the messages. */
export function onPinsChanged(cx: Cx, p: ServerEventPayloads['pins_changed']) {
  const { cs } = cx
  signalPins(cx, p.channel_id)
  const rest = cs.pinNotices.value.filter(n => n.message_id !== p.message_id)
  if (!p.pinned) {
    cs.pinNotices.value = rest
    return
  }
  const note = { id: ++cs.pinNoticeSeq, channel_id: p.channel_id, message_id: p.message_id, actor_name: p.actor_name }
  cs.pinNotices.value = [...rest, note].slice(-MAX_PIN_NOTICES)
}

/** A pinned message that is deleted drops out of an open pinned list. */
export function onPinnedMaybeDeleted(cx: Cx, channelId: string) {
  signalPins(cx, channelId)
}

export function dismissPinNotice(cx: Cx, id: number) {
  cx.cs.pinNotices.value = cx.cs.pinNotices.value.filter(n => n.id !== id)
}
