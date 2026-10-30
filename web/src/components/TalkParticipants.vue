<script setup lang="ts">
import type { PropType } from 'vue'
import type { TalkParticipant } from './presentationTypes'
// "N participants" in the Talk header; opens who is in the Talk and since when.
import { ref, nextTick } from 'vue'
import { Users } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import TalkParticipantList from './TalkParticipantList.vue'
import { useDismissable } from '../composables/useDismissable'

defineProps({
  users: { type: Array as PropType<TalkParticipant[]>, required: true },
  // When the room got its first member (ISO), for the total time.
  startedAt: { type: String, default: '' },
  /** The leader of a temporary Talk, marked in the list. */
  leaderId: { type: String, default: '' }
})

const chatStore = useChatStore()
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
</script>

<template>
  <div ref="root" class="relative flex items-center gap-2">
    <button
      ref="button"
      type="button"
      data-testid="talk-participants"
      aria-haspopup="menu"
      :aria-expanded="open ? 'true' : 'false'"
      v-tooltip="$t('talk.participantsDetails')"
      class="text-xs leading-4 px-1.5 max-md:relative max-md:after:absolute max-md:after:-inset-x-1 max-md:after:-inset-y-3 rounded-full border border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent whitespace-nowrap shrink-0 hover:bg-mnema-accent/20 focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-mnema-accent"
      :aria-label="$t('talk.participants', { count: users.length })"
      @click="toggle"
    >
      <!-- Phones: just the count, so the Talk's name keeps the room. -->
      <span class="max-[420px]:hidden">{{ $t('talk.participants', { count: users.length }) }}</span>
      <span class="hidden items-center gap-1 max-[420px]:inline-flex" aria-hidden="true"><Users class="h-3 w-3" />{{ users.length }}</span>
    </button>

    <TalkParticipantList
      v-if="open"
      data-testid="talk-participants-list"
      class="absolute left-0 top-full z-40 mt-2 w-[280px] max-md:fixed max-md:inset-x-2 max-md:top-14 max-md:mt-0 max-md:w-auto max-h-[min(420px,calc(100dvh-80px))] overflow-y-auto overscroll-contain rounded-xl bg-mnema-elevated p-1.5 shadow-popover pop-in"
      :users="users"
      :started-at="startedAt"
      :leader-id="leaderId"
      @select="openProfile"
    />
  </div>
</template>
