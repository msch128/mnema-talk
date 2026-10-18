import type { Message } from '../../types/domain'
import { decodeThread } from '../../types/domain'
import { api } from '../../lib/api'
import type { ThreadRoot } from './types'
import type { Cx } from './state'
import { sendMessage, uploadMedia } from './messageActions'

export async function openThread(cx: Cx, msg: Message | ThreadRoot | string) {
  const { cs } = cx
  if (!msg) return
  const id = typeof msg === 'string' ? msg : msg.id
  if (!id) return
  const gen = ++cs.threadGen
  cs.activeThread.value = typeof msg === 'object' ? msg : { id }
  cs.threadReplies.value = []
  cs.isThreadLoading.value = true
  try {
    const data = await api(`/api/messages/${id}/thread`, { decode: decodeThread })
    if (gen !== cs.threadGen || cs.activeThread.value?.id !== id) return
    cs.activeThread.value = data.root
    cs.threadReplies.value = data.replies
  } catch (e) {
    if (gen === cs.threadGen) console.error('Failed to load thread:', e)
  } finally {
    if (gen === cs.threadGen) cs.isThreadLoading.value = false
  }
}

export function closeThread(cx: Cx) {
  const { cs } = cx
  cs.threadGen++
  cs.activeThread.value = null
  cs.threadReplies.value = []
  cs.isThreadLoading.value = false
}

export async function sendThreadReply(cx: Cx, content: string, replyToId: string | null = null) {
  const { cs } = cx
  if (!cs.activeThread.value) return null
  return sendMessage(cx, content, cs.activeThread.value.id, replyToId)
}

export async function uploadThreadMedia(cx: Cx, file: File, content = '', replyToId: string | null = null) {
  const { cs } = cx
  if (!cs.activeThread.value) return null
  return uploadMedia(cx, file, content, cs.activeThread.value.id, replyToId)
}

