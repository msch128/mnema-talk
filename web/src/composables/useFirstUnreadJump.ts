// The "Jump to first unread" pill: shown while the "new since" divider is out
// of sight above the viewport, or not loaded at all (the unread run is longer
// than the page that was fetched).
import { ref, computed, watch, nextTick, onScopeDispose, type Ref } from 'vue'
import { api } from '../lib/api'
import { firstUnreadId } from '../lib/chatLogic'
import { decodeChatFirstUnreadResponse } from '../types/rest'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'

const UNREAD_DIVIDER = '[data-unread-divider]'

interface Options {
  container: Ref<HTMLElement | null>
  /** Last-read time captured when the channel was opened. */
  since: Ref<string | null>
  /** The loaded message the divider sits before, if the window holds it. */
  dividerBeforeId: Ref<string | null>
}

export function useFirstUnreadJump({ container, since, dividerBeforeId }: Options) {
  const chatStore = useChatStore()
  const authStore = useAuthStore()

  // Oldest unread message when it is older than the loaded window.
  const remoteId = ref<string | null>(null)
  const dividerAbove = ref(false)
  let lookup = 0

  // The loaded window starts inside the unread run: the divider's place is above it.
  const startsInsideRun = computed(() =>
    !dividerBeforeId.value && chatStore.hasMoreBefore
    && firstUnreadId(chatStore.messages, since.value, authStore.user?.id, false) !== null)

  async function lookUpFirstUnread(channelId: string, at: string) {
    const mine = ++lookup
    remoteId.value = null
    try {
      const path = `/api/channels/${channelId}/first-unread?since=${encodeURIComponent(at)}`
      const res = await api(path, { decode: decodeChatFirstUnreadResponse })
      if (mine === lookup) remoteId.value = res.message_id
    } catch {
      // Older server or desktop client without the route: no pill, nothing else breaks.
      if (mine === lookup) remoteId.value = null
    }
  }

  watch(
    () => [chatStore.activeChannel?.id, since.value, startsInsideRun.value] as const,
    ([channelId, at, needed]) => {
      if (channelId && at && needed) void lookUpFirstUnread(channelId, at)
      else if (!needed) { lookup++; remoteId.value = null }
    },
    { immediate: true }
  )

  function updateDividerAbove() {
    const root = container.value
    const divider = root?.querySelector<HTMLElement>(UNREAD_DIVIDER)
    dividerAbove.value = !!root && !!divider && divider.getBoundingClientRect().bottom < root.getBoundingClientRect().top
  }
  watch(container, (el, _old, onCleanup) => {
    el?.addEventListener('scroll', updateDividerAbove, { passive: true })
    onCleanup(() => el?.removeEventListener('scroll', updateDividerAbove))
  }, { immediate: true })
  watch(() => [dividerBeforeId.value, chatStore.messages.length], () => { void nextTick(updateDividerAbove) })
  onScopeDispose(() => { lookup++ })

  const target = computed(() => (since.value ? dividerBeforeId.value ?? remoteId.value : null))
  const showJumpToUnread = computed(() => !!target.value && (!dividerBeforeId.value || dividerAbove.value))

  async function jumpToFirstUnread() {
    const id = target.value
    if (!id) return
    const divider = container.value?.querySelector<HTMLElement>(UNREAD_DIVIDER)
    if (dividerBeforeId.value && divider) {
      divider.scrollIntoView({ block: 'start' })
      updateDividerAbove()
      return
    }
    await chatStore.jumpToMessage(id)
  }

  return { showJumpToUnread, jumpToFirstUnread }
}
