// What the chat does with the calls of direct messages: whom a call rings, the
// operating system's notice of it, and where a click on that notice leads. The
// state itself is stores/dmCalls; the server's rules are in doc/talks.md.
import type { DmCallEnd, DmCallRing } from '../../types/dmEvents'
import type { Message } from '../../types/domain'
import { t } from '../../i18n'
import { shouldNotify } from '../../lib/chatLogic'
import { isQuiet } from '../../lib/presence'
import { platform } from '../../lib/platform'
import { nameOf } from '../../lib/dm'
import { pushOnThisDevice } from '../../lib/webPush'
import { useBlocksStore } from '../blocks'
import { useDmCallsStore, type CallInfo } from '../dmCalls'
import type { Cx } from './state'
import { goToMessage } from './messageWindow'
import { isTabVisible } from './readState'
import { notificationLevel } from './notifySettings'

/** The shown name of a conversation member (or the community member), or ''. */
export function memberName(cx: Cx, conversationId: string, userId: string): string {
  const member = cx.cs.dms.value.find(d => d.id === conversationId)?.members.find(m => m.id === userId) ?? cx.cs.members.value.find(m => m.id === userId)
  return member ? nameOf(member) : ''
}

/** Brings the member to the call: the conversation opens at its invitation card, which has the Join button. */
export function openCall(cx: Cx, call: CallInfo) {
  goToMessage(cx, { id: call.messageId, channel_id: call.conversationId })
}

function windowAway(): boolean {
  return !isTabVisible() || (typeof document !== 'undefined' && !document.hasFocus())
}

/**
 * The operating system's notice of an incoming call. The page itself shows the
 * call card, so the notice is for a member who is not looking at it. With Web
 * Push on, a hidden page leaves it to the service worker.
 */
export function notifyIncomingCall(cx: Cx, call: CallInfo) {
  if (pushOnThisDevice.value && !isTabVisible()) return
  const allowed = shouldNotify({
    permission: platform.notificationPermission(),
    level: notificationLevel(cx, call.conversationId),
    isMention: true,
    isDirectMention: true,
    hidden: windowAway(),
    isCurrentChannel: call.conversationId === cx.cs.activeChannel.value?.id
  })
  if (!allowed || !windowAway()) return
  platform.notify({
    title: t('call.notifyTitle', { name: memberName(cx, call.conversationId, call.callerId) }),
    body: t('call.notifyBody'),
    channelId: call.conversationId,
    messageId: call.messageId,
    call: true,
    onClick: action => {
      openCall(cx, call)
      // The toast's Join button (a host with notify_call): the page joins as well.
      if (action === 'join') void import('../../composables/useDmCall').then(m => m.useDmCall().join(call.talkId))
    }
  })
}

/** How long the page waits for the ring of an invitation card before it announces the card itself. */
const RING_GRACE_MS = 1500

/**
 * The invitation card of a call arrived as a message. A ring follows it (and
 * announces the call) unless somebody else is in the Talk already, when nothing
 * rings; then the card is announced like a call after all, so a member who is
 * away still hears of it.
 */
export function announceInviteUnlessRung(cx: Cx, msg: Message) {
  const talkId = msg.embeds?.find(e => e.talk_id)?.talk_id
  if (!talkId || !msg.user_id) return
  setTimeout(() => {
    if (useDmCallsStore().active[msg.channel_id]?.messageId === msg.id) return
    notifyIncomingCall(cx, { conversationId: msg.channel_id, talkId, messageId: msg.id, callerId: msg.user_id, talkName: '' })
  }, RING_GRACE_MS)
}

/** Rings the member for `call`, unless they blocked the caller or muted the conversation. */
function ring(cx: Cx, call: CallInfo, seconds: number, announce: boolean) {
  const calls = useDmCallsStore()
  if (call.callerId === cx.authStore.user?.id) {
    calls.ringOutgoing(call, seconds)
    return
  }
  if (useBlocksStore().isBlocked(call.callerId)) return
  if (notificationLevel(cx, call.conversationId) === 'mute') {
    calls.track(call)
    return
  }
  const quiet = isQuiet(cx.authStore.user?.presence)
  calls.ringIncoming(call, seconds, quiet || cx.voiceStore.isConnected)
  if (announce && !quiet) notifyIncomingCall(cx, call)
}

/** dm_call_ring: somebody started a Talk in a conversation of mine. */
export function onDmCallRing(cx: Cx, p: DmCallRing) {
  ring(cx, {
    conversationId: p.conversation_id, talkId: p.talk_id, messageId: p.message_id, callerId: p.caller_id, talkName: p.talk_name
  }, p.ring_seconds, true)
}

/** dm_call_end: the call stopped ringing (for everybody, wherever it was answered) or is over. */
export function onDmCallEnd(_cx: Cx, p: DmCallEnd) {
  useDmCallsStore().end(p)
  platform.closeNotification(p.conversation_id)
}

/** The conversation list was read: the calls it names replace what is held. */
export function syncDmCalls(cx: Cx) {
  useDmCallsStore().syncList(cx.cs.dms.value, cx.authStore.user?.id, (call, seconds) => ring(cx, call, seconds, false))
}
