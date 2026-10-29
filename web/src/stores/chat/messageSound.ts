import type { Message } from '../../types/domain'
import { shouldPlayMessageSound, MESSAGE_SOUND_GAP_MS } from '../../lib/messageSound'
import { isQuiet } from '../../lib/presence'
import { playSoundEffect } from '../../lib/soundEffects'
import { useVoiceStore } from '../voice'
import type { Cx } from './state'
import { isDm } from './dms'
import { messageMentionsMeDirectly, threadAllows } from './notifications'
import { notificationLevel } from './notifySettings'
import { isTabVisible } from './readState'

let lastPlayedAt = 0

function windowFocused(): boolean {
  return typeof document === 'undefined' || document.hasFocus()
}

/**
 * The calm sound for a new message from someone else, if the member's
 * "Message sounds" choice and the notification rules ask for it (see
 * shouldPlayMessageSound). The global sound switch and volume apply in
 * playSoundEffect. osNotified: an OS notification was raised, which brings
 * the system's own sound.
 */
export function playMessageSoundFor(cx: Cx, msg: Message, isMention: boolean, osNotified: boolean) {
  const now = Date.now()
  if (now - lastPlayedAt < MESSAGE_SOUND_GAP_MS) return
  const play = shouldPlayMessageSound({
    mode: useVoiceStore().messageSounds,
    isOwn: msg.user_id === cx.authStore.user?.id,
    quiet: isQuiet(cx.authStore.user?.presence),
    level: notificationLevel(cx, msg.channel_id),
    isDm: isDm(cx, msg.channel_id),
    isMention,
    isDirectMention: messageMentionsMeDirectly(cx, msg),
    threadSilenced: !threadAllows(cx, msg),
    viewing: msg.channel_id === cx.cs.activeChannel.value?.id && isTabVisible() && windowFocused(),
    osNotified
  })
  if (!play) return
  lastPlayedAt = now
  playSoundEffect('message')
}

/** Forgets the last sound (tests). */
export function resetMessageSoundGap() {
  lastPlayedAt = 0
}
