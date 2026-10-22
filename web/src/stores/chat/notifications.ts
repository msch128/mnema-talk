import type { Message } from '../../types/domain'
import { t } from '../../i18n'
import { mentionsUser, shouldNotify } from '../../lib/chatLogic'
import { isQuiet } from '../../lib/presence'
import { platform } from '../../lib/platform'
import type { Cx } from './state'
import { goToMessage } from './messageWindow'
import { isTabVisible } from './readState'
import { notificationLevel } from './notifySettings'

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
  if (isQuiet(cx.authStore.user?.presence)) return false
  return shouldNotify({
    permission: platform.notificationPermission(),
    level: notificationLevel(cx, msg.channel_id),
    isMention,
    hidden: !isTabVisible(),
    isCurrentChannel: msg.channel_id === cx.cs.activeChannel.value?.id
  })
}

export function triggerBrowserNotification(cx: Cx, msg: Message, isMention: boolean) {
  if (!canShowBrowserNotification(cx, msg, isMention)) return
  platform.notify({
    title: buildNotificationTitle(cx, msg),
    body: buildNotificationBody(msg),
    channelId: msg.channel_id,
    messageId: msg.id,
    onClick: () => goToMessage(cx, msg),
  })
}

/** What the platform allows now: the desktop client's operating system can answer differently than at start. */
export async function refreshNotificationPermission(cx: Cx) {
  const result = await platform.refreshNotificationPermission()
  if (result !== 'unsupported') cx.cs.notificationPermission.value = result
  return result
}

export async function requestNotificationPermission(cx: Cx) {
  const { cs } = cx
  const result = await platform.requestNotificationPermission()
  if (result !== 'unsupported') cs.notificationPermission.value = result
  return result
}
