<script setup lang="ts">
import type { PropType } from 'vue'
import type { AvatarUser } from './presentationTypes'
// "Eye N" on a screen share: how many in the Talk watch it. Hovering or a
// click shows who (avatars and names); the sharer sees it on their own share.
import { ref, computed, onUnmounted, useId } from 'vue'
import { Eye } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useDismissable } from '../composables/useDismissable'
import { t } from '../i18n'
import UserAvatar from './UserAvatar.vue'

const props = defineProps({
  // The sharer: whose screen share this is.
  userId: { type: String, required: true },
  // 'stage' sits on the dark video, 'card' on a screen-share card.
  variant: { type: String as PropType<'stage' | 'card'>, default: 'stage' },
  // Also show "0" (my own share: nobody watches yet).
  showZero: { type: Boolean, default: false }
})

const voiceStore = useVoiceStore()
const chatStore = useChatStore()
const authStore = useAuthStore()

function userById(id: string): AvatarUser & { id: string } {
  const room = voiceStore.channelUsers[voiceStore.currentChannelId ?? ''] || {}
  if (room[id]) return room[id]
  if (id === authStore.user?.id) return authStore.user
  return chatStore.members?.find(m => m.id === id) || { id, username: '?' }
}

const viewers = computed(() => voiceStore.viewersOf(props.userId).map(userById))
const count = computed(() => viewers.value.length)
const nameOf = (u: AvatarUser) => u.display_name || u.username
const label = computed(() => (count.value
  ? t('talk.viewers', { count: count.value, names: viewers.value.map(nameOf).join(', ') })
  : t('talk.viewersNone')))

// Open while hovered, or pinned by a click until a click elsewhere / Escape.
const hovered = ref(false)
const pinned = ref(false)
const open = computed(() => hovered.value || pinned.value)
const root = ref<HTMLElement | null>(null)
const button = ref<HTMLButtonElement | null>(null)
const popoverId = `screen-viewers-${useId()}`
let hoverTimer: ReturnType<typeof setTimeout> | undefined

useDismissable(root, (e, reason) => {
  pinned.value = false
  hovered.value = false
  if (reason === 'escape') button.value?.focus()
}, { active: open })

function onEnter() {
  clearTimeout(hoverTimer)
  hoverTimer = setTimeout(() => { hovered.value = true }, 120)
}
function onLeave() {
  clearTimeout(hoverTimer)
  hovered.value = false
}
function toggle() {
  clearTimeout(hoverTimer)
  pinned.value = !pinned.value
  if (!pinned.value) hovered.value = false
}
onUnmounted(() => clearTimeout(hoverTimer))
</script>

<template>
  <div
    v-if="count || showZero"
    ref="root"
    class="relative flex"
    data-testid="screen-viewers"
    :data-viewer-count="count"
    @mouseenter="onEnter"
    @mouseleave="onLeave"
    @click.stop
    @dblclick.stop
  >
    <button
      ref="button"
      type="button"
      data-testid="screen-viewers-button"
      :aria-label="label"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-controls="open ? popoverId : undefined"
      :class="[
        'flex items-center gap-1 rounded tabular-nums font-semibold transition focus:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent',
        variant === 'stage'
          ? 'px-1.5 py-0.5 text-xs text-white/90 hover:text-white hover:bg-white/10'
          : 'h-7 px-2 text-xs text-mnema-accent hover:bg-mnema-accent/15'
      ]"
      @click="toggle"
    >
      <Eye class="w-3.5 h-3.5" aria-hidden="true" />
      <span>{{ count }}</span>
    </button>

    <div
      v-if="open"
      :id="popoverId"
      data-testid="screen-viewers-list"
      class="absolute left-0 top-full z-40 mt-1.5 w-56 max-w-[calc(100vw-16px)] rounded-[10px] bg-mnema-elevated p-1.5 text-left shadow-[inset_0_0_0_1px_#2B2F2D,0_12px_32px_rgba(0,0,0,0.5)] select-none"
    >
      <p class="px-2 pb-1 pt-0.5 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">
        {{ count ? $t('talk.viewersTitle', { count }) : $t('talk.viewersNone') }}
      </p>
      <ul v-if="count" class="max-h-48 overflow-y-auto">
        <li
          v-for="u in viewers"
          :key="u.id"
          :data-viewer="u.id"
          class="flex h-9 items-center gap-2 rounded-md px-2 text-sm text-mnema-text"
        >
          <UserAvatar :user="u" size="xs" />
          <span class="truncate font-medium">{{ nameOf(u) }}</span>
        </li>
      </ul>
    </div>
  </div>
</template>
