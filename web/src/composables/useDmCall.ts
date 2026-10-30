// Starting, joining and ending the calls of direct messages. A conversation has
// no call of its own: "Start a Talk" opens a temporary Talk and the server rings
// the other members (doc/talks.md), "Join" is the ordinary join of that Talk.
import { caughtErrorMessage } from '../lib/api'
import { startConversationTalk } from '../lib/talks'
import { navigate } from '../lib/router'
import { t } from '../i18n'
import { useChatStore } from '../stores/chat'
import { useDmCallsStore } from '../stores/dmCalls'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from './useWebRTC'

export function useDmCall() {
  const chat = useChatStore()
  const voice = useVoiceStore()
  const calls = useDmCallsStore()
  const permissions = usePermissionsStore()
  const toasts = useToastStore()
  const { joinVoiceChannel, leaveVoiceChannel } = useWebRTC()

  /** Opens the Talk of a call on screen and joins it. A Talk this client has not heard of yet is waited for. */
  async function join(talkId: string): Promise<boolean> {
    let talk = chat.allChannels.find(c => c.id === talkId && c.type === 'voice')
    if (!talk) {
      await Promise.all([chat.fetchChannels(), permissions.load()])
      talk = chat.allChannels.find(c => c.id === talkId && c.type === 'voice')
    }
    if (!talk) {
      toasts.error(t('call.gone'))
      return false
    }
    calls.joined(talkId)
    void chat.selectChannel(talk)
    void joinVoiceChannel(talkId)
    voice.activeView = 'voice'
    navigate(chat.linkTo(talkId))
    return true
  }

  /** "Start a Talk" in a conversation: the others are rung, the caller joins at once. */
  async function start(conversationId: string): Promise<boolean> {
    try {
      const started = await startConversationTalk(conversationId)
      await Promise.all([chat.fetchChannels(), permissions.load()])
      return await join(started.channel_id)
    } catch (e) {
      toasts.error(caughtErrorMessage(e, t('dm.startTalkFailed')))
      return false
    }
  }

  /** Gives up on the call this member started: leaving the Talk before anybody joined is the cancel. */
  async function hangUp() {
    calls.clearOutgoing()
    await leaveVoiceChannel()
  }

  return { join, start, hangUp }
}
