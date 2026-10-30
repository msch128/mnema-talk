import { confirm } from '../../lib/confirm'
import { t } from '../../i18n'
import { useToastStore } from '../../stores/toast'
import { myUserId } from '../../stores/voice/state'
import type { Cx } from './state'

// Moving a call between devices. A user-initiated join while the same account
// is already in a Talk on another session asks first; the confirmed join goes
// out with `takeover: true`, and the server ends the other session's voice
// (not its connection) once this one is up. See doc/talks.md.

/** How long the "moved" notice stays when it offers to come back. */
const MOVED_NOTICE_MS = 15_000

/** The Talk my account is listed in although this session is not in it, or null. */
export function talkOnAnotherDevice(cx: Cx): string | null {
  const me = myUserId()
  if (!me) return null
  for (const [channelId, members] of Object.entries(cx.voice.channelUsers)) {
    if (channelId !== cx.voice.currentChannelId && members[me]) return channelId
  }
  return null
}

/**
 * Asks whether to move the call here, or null when there is nothing to ask (so
 * the join does not wait a tick). Resolves to whether the user agreed.
 */
export function askTakeover(cx: Cx): Promise<boolean> | null {
  const elsewhere = talkOnAnotherDevice(cx)
  if (!elsewhere) return null
  const name = cx.chat.allChannels.find(c => c.id === elsewhere)?.name ?? ''
  return confirm({
    title: t('voice.takeoverTitle'),
    body: t('voice.takeoverPrompt', { channel: name }),
    confirmLabel: t('voice.takeoverConfirm'),
    cancelLabel: t('common.cancel'),
    danger: false
  })
}

/** voice_kicked 'voice_taken_over': my call moved to another device. Offers to move it back. */
export function announceTakenOver(channelId: string, takeBack: (channelId: string) => void) {
  useToastStore().info(t('voice.takenOver'), {
    duration: MOVED_NOTICE_MS,
    action: { label: t('voice.takenOverBack'), onClick: () => takeBack(channelId) }
  })
}
