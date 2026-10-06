// Layout helpers of the Talk view.

// The participant strip under a stream can be collapsed (Discord's "hide
// members"); the choice is remembered per browser.
export const STRIP_COLLAPSED_KEY = 'mnema.talk.stripCollapsed'

function defaultStorage() {
  try {
    return globalThis.localStorage ?? null
  } catch {
    return null
  }
}

export function loadStripCollapsed(storage = defaultStorage()) {
  try {
    return storage?.getItem(STRIP_COLLAPSED_KEY) === 'true'
  } catch {
    return false
  }
}

export function saveStripCollapsed(collapsed, storage = defaultStorage()) {
  try {
    if (!storage) return false
    if (collapsed) storage.setItem(STRIP_COLLAPSED_KEY, 'true')
    else storage.removeItem(STRIP_COLLAPSED_KEY)
    return true
  } catch {
    return false
  }
}

// The largest box of `aspect` (width / height) that fits into width × height:
// the stage takes the video's shape, without black bands around it.
export function fitAspect(width, height, aspect = 16 / 9) {
  const ar = Number.isFinite(aspect) && aspect > 0 ? aspect : 16 / 9
  if (!(width > 0) || !(height > 0)) return { width: 0, height: 0 }
  let w = width
  let h = w / ar
  if (h > height) {
    h = height
    w = h * ar
  }
  return { width: Math.floor(w), height: Math.floor(h) }
}

// Below this width (px) of the Talk area the control bar shows icons only and
// moves its secondary buttons into a "more" menu.
export const COMPACT_BAR_WIDTH = 600
