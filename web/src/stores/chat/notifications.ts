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

export function triggerBrowserNotification(cx: Cx, msg: Message, isMention: boolean) {
  const { cs, authStore } = cx
  if (typeof Notification === 'undefined') return
  // Do not disturb and focus silence everything.
  if (isQuiet(authStore.user?.presence)) return
  if (!shouldNotify({
    permission: Notification.permission,
    level: notificationLevel(cx, msg.channel_id),
    isMention,
    hidden: !isTabVisible(),
    isCurrentChannel: msg.channel_id === cs.activeChannel.value?.id
  })) return

  const channel = cs.allChannels.value.find(c => c.id === msg.channel_id)
  const channelName = channel ? `#${channel.name}` : ''
  const sender = msg.display_name || msg.username || ''
  const title = channelName ? `${sender} (${channelName})` : sender

  try {
    const n = new Notification(title, {
      body: msg.content || (msg.attachments?.length ? t('chat.attachment') : ''),
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

