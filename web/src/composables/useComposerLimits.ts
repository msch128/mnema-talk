import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { MAX_MESSAGE_LENGTH, lengthState, slowModeRemaining } from '../lib/composerLimits'
import { formatRemaining } from '../lib/moderation'
import { isApiError } from '../lib/api'
import { t } from '../i18n'

export interface ComposerHint { text: string; blocks: boolean; tone: 'calm' | 'warn' }

/**
 * The limits a member runs into while writing, as one calm line under the
 * composer: the wait of slow mode (counted down from their own last message
 * or the server's answer) and the length of the message. Members who may
 * manage messages in the channel are exempt from slow mode, as on the server.
 */
export function useComposerLimits(opts: {
  channelId: () => string | undefined
  slowModeSeconds: () => number
  exempt: () => boolean
  text: () => string
}) {
  // When each channel lets this member write again, in ms since the epoch.
  const readyAt = reactive<Record<string, number>>({})
  const now = ref(Date.now())
  let tick: ReturnType<typeof setInterval> | undefined

  function keepTicking() {
    if (tick) return
    tick = setInterval(() => {
      now.value = Date.now()
      if (!Object.values(readyAt).some(at => at > now.value)) stopTicking()
    }, 500)
  }
  function stopTicking() {
    clearInterval(tick)
    tick = undefined
  }
  onBeforeUnmount(stopTicking)

  function wait(channelId: string | undefined, ms: number) {
    if (!channelId || ms <= 0) return
    now.value = Date.now()
    readyAt[channelId] = now.value + ms
    keepTicking()
  }

  /** A message went out: in a slow channel the next one has to wait. */
  function sent(channelId: string | undefined) {
    if (opts.slowModeSeconds() > 0 && !opts.exempt()) wait(channelId, opts.slowModeSeconds() * 1000)
  }

  /** A refused send: the server's own wait replaces the estimate. */
  function refused(channelId: string | undefined, error: unknown) {
    if (isApiError(error) && error.slowMode && error.retryAfter !== null) wait(channelId, error.retryAfter * 1000)
  }

  const remainingMs = computed(() => {
    const id = opts.channelId()
    return opts.exempt() || opts.slowModeSeconds() <= 0 || !id ? 0 : slowModeRemaining(readyAt[id], now.value)
  })

  const hint = computed<ComposerHint | null>(() => {
    const length = lengthState(opts.text())
    if (length?.over) return { text: t('chat.limits.tooLong', { count: -length.left, max: MAX_MESSAGE_LENGTH }), blocks: true, tone: 'warn' }
    if (remainingMs.value > 0) return { text: t('chat.limits.slowWait', { time: formatRemaining(remainingMs.value) }), blocks: true, tone: 'calm' }
    if (length) return { text: t('chat.limits.charsLeft', { count: length.left }), blocks: false, tone: 'calm' }
    return null
  })

  return { hint, sent, refused }
}

/** The limits of the open channel for the member: its slow mode (unless they manage messages there) and the message length. */
export function useChannelLimits(text: () => string) {
  const chat = useChatStore()
  const permissions = usePermissionsStore()
  return useComposerLimits({
    channelId: () => chat.activeChannel?.id,
    slowModeSeconds: () => chat.activeChannel?.slow_mode_seconds ?? 0,
    exempt: () => permissions.canIn(chat.activeChannel?.id ?? '', 'manage_messages'),
    text
  })
}
