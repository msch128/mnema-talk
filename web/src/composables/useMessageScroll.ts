// Scrolling of the message list: stick to the bottom, infinite history in both
// directions, jump-to-message with a highlight, and keeping the visible rows in
// place when rows are added above or below. Used by ChatArea.
import { ref, nextTick, watch, onMounted, onUnmounted, type Ref } from 'vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'

interface ScrollAnchor { id: string | undefined; offset: number }

// Start loading the next page when this close (px) to either end.
const LOAD_THRESHOLD_PX = 600
// "At the bottom" for auto-stick on new messages.
const STICK_THRESHOLD_PX = 80

function distanceFromBottom(el: HTMLElement) {
  return el.scrollHeight - el.scrollTop - el.clientHeight
}

function rowsIn(el: HTMLElement | null) {
  return el ? el.querySelectorAll<HTMLElement>('[data-msg-id]') : []
}

function findRow(el: HTMLElement | null, id: string | undefined) {
  for (const row of rowsIn(el)) if (row.dataset.msgId === id) return row
  return null
}

// Remember where the first visible messages sit so the view can be put back
// exactly after rows are added/removed above or below (prepend, trims).
function captureAnchors(el: HTMLElement | null) {
  if (!el) return []
  const top = el.getBoundingClientRect().top
  const anchors: ScrollAnchor[] = []
  for (const row of rowsIn(el)) {
    const rect = row.getBoundingClientRect()
    if (rect.bottom <= top) continue
    anchors.push({ id: row.dataset.msgId, offset: rect.top - top })
    if (anchors.length >= 5) break
  }
  return anchors
}

function restoreAnchors(el: HTMLElement | null, anchors: ScrollAnchor[]) {
  if (!el) return
  const top = el.getBoundingClientRect().top
  for (const a of anchors) {
    const row = findRow(el, a.id)
    if (!row) continue
    const delta = row.getBoundingClientRect().top - top - a.offset
    if (delta) el.scrollTop += delta
    return
  }
}

function canLoadOlder(el: HTMLElement, chatStore: ReturnType<typeof useChatStore>): boolean {
  if (el.scrollTop >= LOAD_THRESHOLD_PX) return false
  return chatStore.hasMoreBefore && !chatStore.isLoadingBefore
}

function canLoadNewer(el: HTMLElement, chatStore: ReturnType<typeof useChatStore>): boolean {
  if (distanceFromBottom(el) >= LOAD_THRESHOLD_PX) return false
  return chatStore.hasMoreAfter && !chatStore.isLoadingAfter
}

function shouldCheckScrollEdges(el: HTMLElement | null, chatStore: ReturnType<typeof useChatStore>): el is HTMLElement {
  if (!el || !chatStore.messages.length) return false
  return !chatStore.isLoadingWindow
}

/** Loads the next page of history when the view is close to either end. */
function loadMoreNearEdges(el: HTMLElement | null, chatStore: ReturnType<typeof useChatStore>) {
  if (!shouldCheckScrollEdges(el, chatStore)) return
  if (canLoadOlder(el, chatStore)) {
    chatStore.loadOlder()
  }
  if (canLoadNewer(el, chatStore)) {
    chatStore.loadNewer()
  }
}

/** The briefly highlighted message after a jump. */
function useHighlight() {
  const highlightedId = ref<string | null>(null)
  let highlightTimer: ReturnType<typeof setTimeout> | undefined

  function flashMessage(id: string) {
    clearTimeout(highlightTimer)
    highlightedId.value = null
    // Re-render without the class first so the animation restarts on repeat jumps.
    requestAnimationFrame(() => {
      highlightedId.value = id
      highlightTimer = setTimeout(() => { highlightedId.value = null }, 2000)
    })
  }
  return { highlightedId, flashMessage, clearHighlightTimer: () => clearTimeout(highlightTimer) }
}

/**
 * Whether the user is parked at the bottom. Kept across layout changes
 * (window resize, panel drag, member list / thread toggle, reply bar) so a
 * shrinking viewport doesn't leave the newest message behind the composer.
 */
function useStickyBottom(container: Ref<HTMLElement | null>) {
  let stickToBottom = true
  let lastSize = { w: 0, h: 0 }
  let resizeObserver: ResizeObserver | null = null

  function sizeChangedSinceObserved(el: HTMLElement) {
    return el.clientWidth !== lastSize.w || el.clientHeight !== lastSize.h
  }

  function handleContainerResize() {
    const el = container.value
    if (!el) return
    const changed = sizeChangedSinceObserved(el)
    lastSize = { w: el.clientWidth, h: el.clientHeight }
    if (changed && stickToBottom) el.scrollTop = el.scrollHeight
  }

  return {
    pin() { stickToBottom = true },
    // Scroll events caused by a pending resize (scrollTop clamping) must not
    // clear the stick flag before the ResizeObserver re-pins the view.
    syncFromScroll(el: HTMLElement, atBottom: boolean) {
      if (!sizeChangedSinceObserved(el)) stickToBottom = atBottom
    },
    observe() {
      if (typeof ResizeObserver === 'undefined' || !container.value) return
      resizeObserver = new ResizeObserver(handleContainerResize)
      resizeObserver.observe(container.value)
    },
    disconnect() { resizeObserver?.disconnect() }
  }
}

/** What a window change of the message list needs the view to do afterwards. */
interface WindowScrollOps {
  isAtBottom: () => boolean
  container: () => HTMLElement | null
  scrollToBottomNow: () => void
  scrollToMessage: (id: string) => void
  markUnread: () => void
}

function isOwnLiveMessage(live: boolean, lastUserId: string | undefined, currentUserId: string | undefined): boolean {
  if (!live || !lastUserId) return false
  return lastUserId === currentUserId
}

function shouldStickToBottom(latest: boolean, live: boolean, wasAtBottom: boolean, isMine: boolean): boolean {
  if (latest) return true
  if (!live) return false
  return wasAtBottom || isMine
}

interface ApplyWindowScrollArgs {
  ops: WindowScrollOps
  chatStore: ReturnType<typeof useChatStore>
  anchors: ScrollAnchor[]
  live: boolean
  jump: { id: string } | null
  stickBottom: boolean
}

function applyWindowScroll({ ops, chatStore, anchors, live, jump, stickBottom }: ApplyWindowScrollArgs) {
  if (jump) {
    ops.scrollToMessage(jump.id)
  } else if (stickBottom) {
    ops.scrollToBottomNow()
  } else {
    restoreAnchors(ops.container(), anchors)
    if (live) ops.markUnread()
  }
  loadMoreNearEdges(ops.container(), chatStore)
}

// One pre-render hook for every window change: decide what caused it, then
// after the DOM updated scroll to the bottom, to a jump target, or put the
// previously visible messages back where they were.
function watchWindowChanges(ops: WindowScrollOps) {
  const chatStore = useChatStore()
  const authStore = useAuthStore()
  let seenLatest = chatStore.latestLoadSeq
  let seenLive = chatStore.liveAppendSeq
  let seenJump = chatStore.jumpTarget?.seq ?? 0

  watch(
    () => [chatStore.messages, chatStore.latestLoadSeq, chatStore.liveAppendSeq, chatStore.jumpTarget],
    () => {
      const anchors = captureAnchors(ops.container())
      const wasAtBottom = ops.isAtBottom()
      const latest = chatStore.latestLoadSeq !== seenLatest
      const live = chatStore.liveAppendSeq !== seenLive
      const jumpTarget = chatStore.jumpTarget
      const jump = jumpTarget && jumpTarget.seq !== seenJump ? jumpTarget : null
      seenLatest = chatStore.latestLoadSeq
      seenLive = chatStore.liveAppendSeq
      seenJump = jumpTarget?.seq ?? 0

      nextTick(() => {
        const last = chatStore.messages[chatStore.messages.length - 1]
        const isMine = isOwnLiveMessage(live, last?.user_id, authStore.user?.id)
        const stickBottom = shouldStickToBottom(latest, live, wasAtBottom, isMine)
        applyWindowScroll({ ops, chatStore, anchors, live, jump, stickBottom })
      })
    },
    { flush: 'pre' }
  )
}

export function useMessageScroll(container: Ref<HTMLElement | null>) {
  const chatStore = useChatStore()
  const showUnreadPill = ref(false)
  const { highlightedId, flashMessage, clearHighlightTimer } = useHighlight()
  let scrollFrame = 0

  const sticky = useStickyBottom(container)

  function isAtBottom() {
    const el = container.value
    return !el || distanceFromBottom(el) <= STICK_THRESHOLD_PX
  }

  const messageRows = () => rowsIn(container.value)

  function scrollToBottomNow() {
    const el = container.value
    if (el) el.scrollTop = el.scrollHeight
    sticky.pin()
    showUnreadPill.value = false
  }

  function scrollToBottom() {
    nextTick(scrollToBottomNow)
  }

  function scrollToMessage(id: string) {
    const row = findRow(container.value, id)
    if (!row) return
    row.scrollIntoView({ block: 'center' })
    flashMessage(id)
  }

  function handleScroll() {
    if (scrollFrame) return
    scrollFrame = requestAnimationFrame(() => {
      scrollFrame = 0
      const el = container.value
      // Scroll events caused by a pending resize (scrollTop clamping) must not
      // clear the stick flag before the ResizeObserver re-pins the view.
      if (el) sticky.syncFromScroll(el, isAtBottom())
      if (isAtBottom() && !chatStore.hasMoreAfter) showUnreadPill.value = false
      loadMoreNearEdges(container.value, chatStore)
    })
  }

  watchWindowChanges({
    isAtBottom,
    container: () => container.value,
    scrollToBottomNow,
    scrollToMessage,
    markUnread: () => { showUnreadPill.value = true }
  })

  // The channel changed: nothing is unread relative to the new view.
  function resetUnreadPill() {
    showUnreadPill.value = false
  }

  onMounted(() => {
    scrollToBottom()
    sticky.observe()
  })

  onUnmounted(() => {
    sticky.disconnect()
    clearHighlightTimer()
    if (scrollFrame) cancelAnimationFrame(scrollFrame)
  })

  return {
    showUnreadPill, highlightedId, messageRows, scrollToBottom, scrollToBottomNow,
    handleScroll, resetUnreadPill
  }
}
