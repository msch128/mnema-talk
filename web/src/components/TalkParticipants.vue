<script setup>
// "N participants" in the Talk header; opens who is in the Talk and since when.
import { ref, nextTick, onBeforeUnmount } from 'vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import UserAvatar from './UserAvatar.vue'
import VoiceTimer from './VoiceTimer.vue'

defineProps({
  users: { type: Array, required: true },
  // When the room got its first member (ISO), for the total time.
  startedAt: { type: String, default: '' }
})

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const open = ref(false)
const root = ref(null)
const button = ref(null)

function onPointerDown(e) {
  if (root.value && !root.value.contains(e.target)) close(false)
}
function show() {
  open.value = true
  document.addEventListener('pointerdown', onPointerDown)
  nextTick(() => root.value?.querySelector('[role="menuitem"]')?.focus())
}
function close(returnFocus = true) {
  open.value = false
  document.removeEventListener('pointerdown', onPointerDown)
  if (returnFocus) button.value?.focus()
}
function toggle() {
  if (open.value) close()
  else show()
}
function openProfile(user) {
  close(false)
  chatStore.openUserProfile(user)
}
function onKeydown(e) {
  const items = [...(root.value?.querySelectorAll('[role="menuitem"]') || [])]
  const i = items.indexOf(document.activeElement)
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    close()
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    items[(i + 1) % items.length]?.focus()
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    items[(i - 1 + items.length) % items.length]?.focus()
  }
}
onBeforeUnmount(() => document.removeEventListener('pointerdown', onPointerDown))
</script>

<template>
  <div ref="root" class="relative flex items-center gap-2" @keydown="onKeydown">
    <button
      ref="button"
      type="button"
      data-testid="talk-participants"
      aria-haspopup="menu"
      :aria-expanded="open ? 'true' : 'false'"
      v-tooltip="$t('talk.participantsDetails')"
      class="text-xs leading-4 px-1.5 rounded-full border border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent whitespace-nowrap flex-shrink-0 hover:bg-mnema-accent/20 focus-visible:outline focus-visible:outline-2 focus-visible:outline-mnema-accent"
      @click="toggle"
    >
      {{ $t('talk.participants', { count: users.length }) }}
    </button>

    <div
      v-if="open"
      role="menu"
      data-testid="talk-participants-list"
      :aria-label="$t('talk.participantsDetails')"
      class="absolute left-0 top-full z-40 mt-2 w-[280px] max-w-[calc(100vw-16px)] rounded-[10px] bg-mnema-elevated p-1.5 shadow-[inset_0_0_0_1px_#2B2F2D,0_12px_32px_rgba(0,0,0,0.5)]"
    >
      <div class="flex items-center justify-between px-2 pb-1.5 pt-1 text-xs text-mnema-tertiary">
        <span class="font-semibold uppercase tracking-wide">{{ $t('talk.participants', { count: users.length }) }}</span>
        <span v-if="startedAt" class="flex items-center gap-1">{{ $t('talk.runningFor') }} <VoiceTimer :since="startedAt" class="text-mnema-text" /></span>
      </div>
      <p v-if="!users.length" class="px-2 py-1.5 text-sm text-mnema-tertiary">{{ $t('voice.noOneInVoice') }}</p>
      <button
        v-for="u in users"
        :key="u.id"
        type="button"
        role="menuitem"
        tabindex="-1"
        :data-participant="u.username"
        class="flex h-10 w-full items-center gap-2.5 rounded-md px-2 text-left text-sm transition-colors hover:bg-mnema-hover focus:outline-none focus-visible:bg-mnema-hover"
        @click="openProfile(u)"
      >
        <UserAvatar :user="u" size="sm" :is-speaking="!!voiceStore.speakingUsers[u.id]" />
        <span class="flex min-w-0 flex-1 flex-col leading-[18px]">
          <span class="truncate font-medium text-mnema-text">{{ u.display_name || u.username }}</span>
          <span class="truncate font-mono text-xs text-mnema-tertiary">@{{ u.username }}</span>
        </span>
        <VoiceTimer :since="u.joined_at" class="text-xs text-mnema-tertiary" />
      </button>
    </div>
  </div>
</template>
