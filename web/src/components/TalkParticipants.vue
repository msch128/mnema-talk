<script setup lang="ts">
import type { PropType } from 'vue'
import type { TalkParticipant } from './presentationTypes'
// "N participants" in the Talk header; opens who is in the Talk and since when.
import { ref, nextTick } from 'vue'
import { Users } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import UserAvatar from './UserAvatar.vue'
import VoiceTimer from './VoiceTimer.vue'
import MuteMarks from './MuteMarks.vue'
import { useDismissable } from '../composables/useDismissable'

defineProps({
  users: { type: Array as PropType<TalkParticipant[]>, required: true },
  // When the room got its first member (ISO), for the total time.
  startedAt: { type: String, default: '' }
})

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const open = ref(false)
const root = ref<HTMLElement | null>(null)
const button = ref<HTMLButtonElement | null>(null)

// A press elsewhere closes quietly; Escape hands focus back to the button.
useDismissable(root, (e, reason) => close(reason === 'escape'), { active: open })

function show() {
  open.value = true
  nextTick(() => root.value?.querySelector<HTMLElement>('[role="menuitem"]')?.focus())
}
function close(returnFocus = true) {
  open.value = false
  if (returnFocus) button.value?.focus()
}
function toggle() {
  if (open.value) close()
  else show()
}
function openProfile(user: TalkParticipant) {
  close(false)
  chatStore.openUserProfile(user)
}
function onKeydown(e: KeyboardEvent) {
  const items = [...(root.value?.querySelectorAll<HTMLElement>('[role="menuitem"]') || [])]
  const i = items.findIndex(el => el === document.activeElement)
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    items[(i + 1) % items.length]?.focus()
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    items[(i - 1 + items.length) % items.length]?.focus()
  }
}
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
      class="text-xs leading-4 px-1.5 rounded-full border border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent whitespace-nowrap shrink-0 hover:bg-mnema-accent/20 focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-mnema-accent"
      :aria-label="$t('talk.participants', { count: users.length })"
      @click="toggle"
    >
      <!-- Phones: just the count, so the Talk's name keeps the room. -->
      <span class="max-[420px]:hidden">{{ $t('talk.participants', { count: users.length }) }}</span>
      <span class="hidden items-center gap-1 max-[420px]:inline-flex" aria-hidden="true"><Users class="h-3 w-3" />{{ users.length }}</span>
    </button>

    <div
      v-if="open"
      role="menu"
      data-testid="talk-participants-list"
      :aria-label="$t('talk.participantsDetails')"
      class="absolute left-0 top-full z-40 mt-2 w-[280px] max-w-[calc(100vw-16px)] max-h-[min(420px,calc(100dvh-80px))] overflow-y-auto overscroll-contain rounded-xl bg-mnema-elevated p-1.5 shadow-popover pop-in"
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
        class="flex h-10 w-full items-center gap-2.5 rounded-md px-2 text-left text-sm transition-colors hover:bg-mnema-hover focus:outline-hidden focus-visible:bg-mnema-hover"
        @click="openProfile(u)"
      >
        <UserAvatar :user="u" size="sm" :is-speaking="voiceStore.isSpeaking(u.id)" />
        <span class="flex min-w-0 flex-1 flex-col leading-[18px]">
          <span class="truncate font-medium text-mnema-text">{{ u.display_name || u.username }}</span>
          <span class="truncate font-mono text-xs text-mnema-tertiary">@{{ u.username }}</span>
        </span>
        <MuteMarks :muted="voiceStore.muteStateOf(u.id).muted" :deafened="voiceStore.muteStateOf(u.id).deafened" />
        <VoiceTimer :since="u.joined_at" class="text-xs text-mnema-tertiary" />
      </button>
    </div>
  </div>
</template>
