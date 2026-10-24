// The Stage of the Talk a member is shown or in: whether it is one, whether
// they may speak, and the actions (hand, talk power, reactions). The server
// decides every one of them again; this is what the interface offers.
import { computed, watch } from 'vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useVoiceStore } from '../stores/voice'
import { createReactionLimiter } from '../lib/stage'
import { isStageTalk } from '../lib/talks'

const allowReaction = createReactionLimiter()

export function useStage(channelId: () => string | null) {
  const voice = useVoiceStore()
  const chat = useChatStore()
  const auth = useAuthStore()
  const permissions = usePermissionsStore()
  const features = useServerFeaturesStore()

  const channel = computed(() => chat.allChannels.find(c => c.id === channelId()) ?? null)
  const isStage = computed(() => features.talksStage && isStageTalk(channel.value))
  const myId = computed(() => auth.user?.id ?? null)
  const canManage = computed(() => isStage.value && permissions.canIn(channelId() ?? '', 'manage_stage'))
  /** Outside a Stage everyone may; in one, holders of Manage stage and members with talk power. */
  const iAmSpeaker = computed(() => !isStage.value || canManage.value || voice.hasTalkPower(channelId(), myId.value))
  const myHandRaised = computed(() => {
    const id = channelId()
    return !!id && !!myId.value && !!voice.channelUsers[id]?.[myId.value]?.hand_raised_at
  })

  function send<K extends 'stage_hand' | 'stage_talk_power'>(type: K, payload: Parameters<typeof chat.sendWSEvent<K>>[1]) {
    chat.sendWSEvent(type, payload)
  }

  /** In a Stage without talk power the microphone is off; it stays off when talk power arrives. */
  function muteWithoutTalkPower(connected: () => boolean) {
    watch(() => connected() && !iAmSpeaker.value, silent => {
      if (silent && !voice.isMuted) voice.toggleMute()
    }, { immediate: true })
  }

  return {
    isStage, canManage, iAmSpeaker, myHandRaised, muteWithoutTalkPower,
    raiseHand: () => send('stage_hand', { raised: true }),
    lowerHand: () => send('stage_hand', { raised: false }),
    dismissHand: (userId: string) => send('stage_hand', { user_id: userId, raised: false }),
    giveTalkPower: (userId: string) => send('stage_talk_power', { user_id: userId, granted: true }),
    takeTalkPower: (userId: string) => send('stage_talk_power', { user_id: userId, granted: false }),
    /** A live reaction; false when over the allowance (nothing is sent). */
    react: (emoji: string) => {
      if (!voice.currentChannelId || !allowReaction()) return false
      chat.sendWSEvent('talk_reaction', { emoji })
      return true
    }
  }
}
