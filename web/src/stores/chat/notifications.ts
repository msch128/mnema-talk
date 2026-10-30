import type { Message } from '../../types/domain'
import { t } from '../../i18n'
import { previewText, replySnippet } from '../../lib/replies'
import { mentionsUser, shouldNotify } from '../../lib/chatLogic'
import { isQuiet } from '../../lib/presence'
import { platform } from '../../lib/platform'
import { pushOnThisDevice } from '../../lib/webPush'
import type { Cx } from './state'
import { usePermissionsStore } from '../permissions'
import { goToMessage } from './messageWindow'
import { isTabVisible } from './readState'
import { notificationLevel } from './notifySettings'
import { notifyShowsText } from '../../lib/notifyPreview'

/** Whether msg mentions the signed-in user (@username, @all, @here) or replies to them. */
export function messageMentionsMe(cx: Cx, msg: Message) {
  const { authStore } = cx
  const me = authStore.user
  if (!me || !msg) return false
  if (msg.reply_to?.user_id === me.id) return true
  if (Array.isArray(msg.mentions)) return msg.mentions.includes(me.id)
  return mentionsUser(msg.content || '', me.username)
}

/** Whether msg is addressed to the signed-in user by name, through one of their roles or as a reply (not by @all/@here). */
export function messageMentionsMeDirectly(cx: Cx, msg: Message): boolean {
  const me = cx.authStore.user
  if (!me || !msg) return false
  if (msg.reply_to?.user_id === me.id) return true
  if (mentionsUser(msg.content || '', me.username)) return true
  const mine = new Set(usePermissionsStore().myRoleIds)
  return (msg.mention_roles ?? []).some(id => mine.has(id))
}

function buildNotificationTitle(cx: Cx, msg: Message): string {
  const channel = cx.cs.allChannels.value.find(c => c.id === msg.channel_id)
  const channelName = channel ? `#${channel.name}` : ''
  const sender = msg.display_name || msg.username || ''
  return channelName ? `${sender} (${channelName})` : sender
}

const NOTIFICATION_BODY_LEN = 140

/** Plain text like a reply preview: no Markdown, spoilers hidden, emoji as :name:. */
export function buildNotificationBody(msg: Message): string {
  const text = replySnippet(previewText(msg.content), NOTIFICATION_BODY_LEN)
  if (text) return msg.poll ? `${t('poll.title')}: ${text}` : text
  return msg.attachments?.length ? t('chat.attachment') : ''
}

/**
 * A thread reply notifies only the followers of its thread, and whoever it
 * addresses by name. Without a list from the server (an older one) every reply
 * is treated as before.
 */
export function threadAllows(cx: Cx, msg: Message): boolean {
  if (!msg.parent_id || !cx.cs.followKnown.value) return true
  return cx.cs.followedThreads.value.has(msg.parent_id) || messageMentionsMeDirectly(cx, msg)
}

function canShowBrowserNotification(cx: Cx, msg: Message, isMention: boolean): boolean {
  if (isQuiet(cx.authStore.user?.presence)) return false
  if (!threadAllows(cx, msg)) return false
  // With push on, a hidden page leaves the notification to the service worker, so it is not shown twice.
  if (pushOnThisDevice.value && !isTabVisible()) return false
  return shouldNotify({
    permission: platform.notificationPermission(),
    level: notificationLevel(cx, msg.channel_id),
    isMention,
    isDirectMention: messageMentionsMeDirectly(cx, msg),
    hidden: !isTabVisible(),
    isCurrentChannel: msg.channel_id === cx.cs.activeChannel.value?.id
  })
}

/** Raises the OS notification when the rules allow it; true when it did (the system then plays its own sound). */
export function triggerBrowserNotification(cx: Cx, msg: Message, isMention: boolean): boolean {
  if (!canShowBrowserNotification(cx, msg, isMention)) return false
  platform.notify({
    title: buildNotificationTitle(cx, msg),
    // Like a push: with the text off (the member's choice, or the operator's), only who wrote and where.
    body: notifyShowsText.value ? buildNotificationBody(msg) : t('chat.notifyNoText'),
    channelId: msg.channel_id,
    messageId: msg.id,
    onClick: () => goToMessage(cx, msg),
  })
  return true
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
