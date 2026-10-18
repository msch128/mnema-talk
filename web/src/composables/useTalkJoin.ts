import { computed } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useWebRTC } from './useWebRTC'
import { confirm } from '../lib/confirm'
import { t } from '../i18n'

/**
 * Which Talk is shown (the given one, else the one the user is in) and
 * joining it. Not connected to it, the stage is a preview and no microphone
 * is requested before the user joins.
 */
export function useTalkJoin(channelId: () => string | null, onJoin: (id: string) => void) {
  const voiceStore = useVoiceStore()
  const chatStore = useChatStore()
  const { joinVoiceChannel } = useWebRTC()

  const shownChannelId = computed(() => channelId() || voiceStore.currentChannelId || null)
  const isConnectedHere = computed(() => voiceStore.isConnected && !!shownChannelId.value && voiceStore.currentChannelId === shownChannelId.value)

  // Resolves to whether the join went ahead (false when the user kept their
  // current Talk in the switch confirmation).
  async function join() {
    const id = shownChannelId.value
    if (!id) return false
    if (voiceStore.warnSwitchChannel && voiceStore.currentChannelId && voiceStore.currentChannelId !== id) {
      const ch = chatStore.allChannels.find(c => c.id === id)
      const name = ch?.name || ''
      const ok = await confirm({
        title: t('audio.switchChannelTitle'),
        body: t('audio.switchChannelPrompt', { channel: name }),
        confirmLabel: t('audio.switchChannelConfirm'),
        cancelLabel: t('common.cancel'),
        danger: false
      })
      if (!ok) return false
    }
    onJoin(id)
    joinVoiceChannel(id)
    return true
  }

  return { shownChannelId, isConnectedHere, join }
}
