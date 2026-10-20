import type { Message } from '../../types/domain'
import { t } from '../../i18n'
import { mentionsUser, shouldNotify } from '../../lib/chatLogic'
import { isQuiet } from '../../lib/presence'
import type { Cx } from './state'
import { goToMessage } from './messageWindow'
import { isTabVisible, notificationLevel } from './readState'

/** Whether msg mentions the signed-in user (@username, @all, @here) or replies to them. */
export function messageMentionsMe(cx: Cx, msg: Message) {
  const { authStore } = cx
  const me = authStore.user
  if (!me || !msg) return false
  if (msg.reply_to?.user_id === me.id) return true
  if (Array.isArray(msg.mentions)) return msg.mentions.includes(me.id)
  return mentionsUser(msg.content || '', me.username)
}

function buildNotificationTitle(cx: Cx, msg: Message): string {
  const channel = cx.cs.allChannels.value.find(c => c.id === msg.channel_id)
  const channelName = channel ? `#${channel.name}` : ''
  const sender = msg.display_name || msg.username || ''
  return channelName ? `${sender} (${channelName})` : sender
}

function buildNotificationBody(msg: Message): string {
  if (msg.content) return msg.content
  return msg.attachments?.length ? t('chat.attachment') : ''
}

function canShowBrowserNotification(cx: Cx, msg: Message, isMention: boolean): boolean {
  if (typeof Notification === 'undefined') return false
  if (isQuiet(cx.authStore.user?.presence)) return false
  return shouldNotify({
    permission: Notification.permission,
    level: notificationLevel(cx, msg.channel_id),
    isMention,
    hidden: !isTabVisible(),
    isCurrentChannel: msg.channel_id === cx.cs.activeChannel.value?.id
  })
}

export function triggerBrowserNotification(cx: Cx, msg: Message, isMention: boolean) {
  if (!canShowBrowserNotification(cx, msg, isMention)) return
  try {
    const n = new Notification(buildNotificationTitle(cx, msg), {
      body: buildNotificationBody(msg),
      icon: '/favicon.svg',
      tag: msg.channel_id
    })
    n.onclick = () => {
      if (typeof window !== 'undefined') window.focus()
      goToMessage(cx, msg)
      n.close()
    }
  } catch (e) {
    console.warn('Browser notification failed:', e)
  }
}

export async function requestNotificationPermission(cx: Cx) {
  const { cs } = cx
  if (typeof Notification === 'undefined') return 'unsupported'
  try {
    cs.notificationPermission.value = await Notification.requestPermission()
  } catch {
    cs.notificationPermission.value = Notification.permission
  }
  return cs.notificationPermission.value
}

