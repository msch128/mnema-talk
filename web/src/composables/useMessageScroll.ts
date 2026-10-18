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

export function useMessageScroll(container: Ref<HTMLElement | null>) {
  const chatStore = useChatStore()
  const authStore = useAuthStore()

  const showUnreadPill = ref(false)
  const highlightedId = ref<string | null>(null)
  let highlightTimer: ReturnType<typeof setTimeout> | undefined
  let scrollFrame = 0

  // Whether the user is parked at the bottom. Kept across layout changes
  // (window resize, panel drag, member list / thread toggle, reply bar) so a
  // shrinking viewport doesn't leave the newest message behind the composer.
  let stickToBottom = true
  let lastSize = { w: 0, h: 0 }
  let resizeObserver: ResizeObserver | null = null

  function distanceFromBottom(el: HTMLElement) {
    return el.scrollHeight - el.scrollTop - el.clientHeight
  }

  function isAtBottom() {
    const el = container.value
    return !el || distanceFromBottom(el) <= STICK_THRESHOLD_PX
  }

  function messageRows() {
    return container.value ? container.value.querySelectorAll<HTMLElement>('[data-msg-id]') : []
  }

  function scrollToBottomNow() {
    const el = container.value
    if (el) el.scrollTop = el.scrollHeight
    stickToBottom = true
    showUnreadPill.value = false
  }

  function scrollToBottom() {
    nextTick(scrollToBottomNow)
  }

  function findRow(id: string | undefined) {
    for (const row of messageRows()) if (row.dataset.msgId === id) return row
    return null
  }

  // Remember where the first visible messages sit so the view can be put back
  // exactly after rows are added/removed above or below (prepend, trims).
  function captureAnchor() {
    const el = container.value
    if (!el) return []
    const top = el.getBoundingClientRect().top
    const anchors: ScrollAnchor[] = []
    for (const row of messageRows()) {
      const rect = row.getBoundingClientRect()
      if (rect.bottom <= top) continue
      anchors.push({ id: row.dataset.msgId, offset: rect.top - top })
      if (anchors.length >= 5) break
    }
    return anchors
  }

  function restoreAnchor(anchors: ScrollAnchor[]) {
    const el = container.value
    if (!el) return
    const top = el.getBoundingClientRect().top
    for (const a of anchors) {
      const row = findRow(a.id)
      if (!row) continue
      const delta = row.getBoundingClientRect().top - top - a.offset
      if (delta) el.scrollTop += delta
      return
    }
  }

  function flashMessage(id: string) {
    clearTimeout(highlightTimer)
    highlightedId.value = null
    // Re-render without the class first so the animation restarts on repeat jumps.
    requestAnimationFrame(() => {
      highlightedId.value = id
      highlightTimer = setTimeout(() => { highlightedId.value = null }, 2000)
    })
  }

  function scrollToMessage(id: string) {
    const row = findRow(id)
    if (!row) return
    row.scrollIntoView({ block: 'center' })
    flashMessage(id)
  }

  function maybeLoadMore() {
    const el = container.value
    if (!el || !chatStore.messages.length || chatStore.isLoadingWindow) return
    if (el.scrollTop < LOAD_THRESHOLD_PX && chatStore.hasMoreBefore && !chatStore.isLoadingBefore) {
      chatStore.loadOlder()
    }
    if (distanceFromBottom(el) < LOAD_THRESHOLD_PX && chatStore.hasMoreAfter && !chatStore.isLoadingAfter) {
      chatStore.loadNewer()
    }
  }

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

  function handleScroll() {
    if (scrollFrame) return
    scrollFrame = requestAnimationFrame(() => {
      scrollFrame = 0
      const el = container.value
      // Scroll events caused by a pending resize (scrollTop clamping) must not
      // clear the stick flag before the ResizeObserver re-pins the view.
      if (el && !sizeChangedSinceObserved(el)) stickToBottom = isAtBottom()
      if (isAtBottom() && !chatStore.hasMoreAfter) showUnreadPill.value = false
      maybeLoadMore()
    })
  }

  // One pre-render hook for every window change: decide what caused it, then
  // after the DOM updated scroll to the bottom, to a jump target, or put the
  // previously visible messages back where they were.
  let seenLatest = chatStore.latestLoadSeq
  let seenLive = chatStore.liveAppendSeq
  let seenJump = chatStore.jumpTarget?.seq ?? 0

  watch(
    () => [chatStore.messages, chatStore.latestLoadSeq, chatStore.liveAppendSeq, chatStore.jumpTarget],
    () => {
      const anchors = captureAnchor()
      const wasAtBottom = isAtBottom()
      const latest = chatStore.latestLoadSeq !== seenLatest
      const live = chatStore.liveAppendSeq !== seenLive
      const jump = chatStore.jumpTarget && chatStore.jumpTarget.seq !== seenJump ? chatStore.jumpTarget : null
      seenLatest = chatStore.latestLoadSeq
      seenLive = chatStore.liveAppendSeq
      seenJump = chatStore.jumpTarget?.seq ?? 0

      nextTick(() => {
        const last = chatStore.messages[chatStore.messages.length - 1]
        const mine = live && last?.user_id && last.user_id === authStore.user?.id
        if (jump) {
          scrollToMessage(jump.id)
        } else if (latest) {
          scrollToBottomNow()
        } else if (live && (wasAtBottom || mine)) {
          scrollToBottomNow()
        } else {
          restoreAnchor(anchors)
          if (live) showUnreadPill.value = true
        }
        maybeLoadMore()
      })
    },
    { flush: 'pre' }
  )

  // The channel changed: nothing is unread relative to the new view.
  function resetUnreadPill() {
    showUnreadPill.value = false
  }

  onMounted(() => {
    scrollToBottom()
    if (typeof ResizeObserver !== 'undefined' && container.value) {
      resizeObserver = new ResizeObserver(handleContainerResize)
      resizeObserver.observe(container.value)
    }
  })

  onUnmounted(() => {
    resizeObserver?.disconnect()
    clearTimeout(highlightTimer)
    if (scrollFrame) cancelAnimationFrame(scrollFrame)
  })

  return {
    showUnreadPill, highlightedId, messageRows, scrollToBottom, scrollToBottomNow,
    handleScroll, resetUnreadPill
  }
}
