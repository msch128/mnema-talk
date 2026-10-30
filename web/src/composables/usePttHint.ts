// The first time a member is in a Talk with push to talk on, one hint says what
// the key can and cannot do: in a browser it works while the window is in front
// and not in a text box, the desktop app has a key that also works over games.
// Without a key chosen it says so and offers to choose one.
import { watch } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { openSettings } from '../lib/settingsDialog'
import { pttKeyLabel } from '../lib/pttKey'
import { readItem, writeItem } from '../lib/voicePlayback'
import { t } from '../i18n'

const SEEN_KEY = 'mnema_ptt_hint_seen'
const HINT_MS = 12000

export function usePttHint() {
  const voice = useVoiceStore()
  const toasts = useToastStore()
  watch(() => voice.isConnected && voice.inputMode === 'ptt', on => {
    if (!on || readItem(SEEN_KEY)) return
    writeItem(SEEN_KEY, '1')
    if (voice.pttKey) {
      toasts.push({ type: 'info', text: t('audio.pttFirstUse', { key: pttKeyLabel(voice.pttKey) }), duration: HINT_MS })
    } else {
      toasts.push({ type: 'info', text: t('audio.pttFirstUseNoKey'), duration: HINT_MS, action: { label: t('audio.pttFirstUseAction'), onClick: () => openSettings('audio') } })
    }
  }, { immediate: true })
}
