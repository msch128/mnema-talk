// The text chat of a Talk (the voice channel's own chat), shown as a panel
// under the stage. Whether it is open is remembered per browser (closed by
// default); /v/:id/chat opens it.

export const VOICE_CHAT_OPEN_KEY = 'mnema.voiceChat.open'

// Its height, resized like the side panels (useResizable, axis 'y'). The
// stage above keeps at least STAGE_MIN_HEIGHT (plus the Talk header).
export const VOICE_CHAT_HEIGHT = { name: 'voiceChat', side: 'bottom', defaultWidth: 300, min: 160, max: 1200 }
export const STAGE_MIN_HEIGHT = 240

function defaultStorage() {
  try {
    return globalThis.localStorage ?? null
  } catch {
    return null
  }
}

/** Whether the panel was open last time (closed by default). */
export function loadVoiceChatOpen(storage = defaultStorage()) {
  try {
    return storage?.getItem(VOICE_CHAT_OPEN_KEY) === '1'
  } catch {
    return false
  }
}

export function saveVoiceChatOpen(open, storage = defaultStorage()) {
  try {
    storage?.setItem(VOICE_CHAT_OPEN_KEY, open ? '1' : '0')
    return true
  } catch {
    return false
  }
}

/**
 * Whether the panel is open after a voice route was applied.
 * - /v/:id/chat always opens it (deep links, the chat button).
 * - /v/:id within the Talk already shown closes it (back button, a link).
 * - /v/:id entering a Talk keeps the remembered state.
 */
export function voiceChatOpenFor({ routeShowChat, sameTalk, remembered }) {
  if (routeShowChat) return true
  if (sameTalk) return false
  return !!remembered
}

/** Badge text for the chat button: unread messages while the panel is closed. */
export function unreadBadge(count) {
  const n = Number(count) || 0
  if (n <= 0) return ''
  return n > 99 ? '99+' : String(n)
}
