<script setup lang="ts">
import type { PropType } from 'vue'
import type { TalkParticipant } from './presentationTypes'
// Who is in a Talk and since when: the list of the "N participants" popover in
// the Talk header and of the overview that a member's profile opens. Arrow
// keys move between the rows; a row selects its member.
import { Camera, MonitorUp } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import UserAvatar from './UserAvatar.vue'
import VoiceTimer from './VoiceTimer.vue'
import MuteMarks from './MuteMarks.vue'
import TalkLeaderMark from './TalkLeaderMark.vue'

defineProps({
  users: { type: Array as PropType<TalkParticipant[]>, required: true },
  // When the room got its first member (ISO), for the total time.
  startedAt: { type: String, default: '' },
  /** The leader of a temporary Talk, marked in the list. */
  leaderId: { type: String, default: '' }
})
const emit = defineEmits<{ select: [user: TalkParticipant] }>()

const voiceStore = useVoiceStore()

function onKeydown(e: KeyboardEvent) {
  const items = [...(e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>('[role="menuitem"]')]
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
  <div role="menu" :aria-label="$t('talk.participantsDetails')" @keydown="onKeydown">
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
      @click="emit('select', u)"
    >
      <UserAvatar :user="u" size="sm" :is-speaking="voiceStore.isSpeaking(u.id)" />
      <span class="flex min-w-0 flex-1 flex-col leading-[18px]">
        <span class="flex min-w-0 items-center gap-1"><span class="truncate font-medium text-mnema-text">{{ u.display_name || u.username }}</span><TalkLeaderMark v-if="leaderId && u.id === leaderId" /></span>
        <span class="truncate font-mono text-xs text-mnema-tertiary">@{{ u.username }}</span>
      </span>
      <span v-if="voiceStore.mediaState[u.id]?.screen" class="shrink-0 text-mnema-accent" role="img" :aria-label="$t('talk.sharingScreen')"><MonitorUp class="h-3.5 w-3.5" /></span>
      <span v-if="voiceStore.mediaState[u.id]?.camera" class="shrink-0 text-mnema-accent" role="img" :aria-label="$t('talk.cameraOn')"><Camera class="h-3.5 w-3.5" /></span>
      <MuteMarks :muted="voiceStore.muteStateOf(u.id).muted" :deafened="voiceStore.muteStateOf(u.id).deafened" />
      <VoiceTimer :since="u.joined_at" class="text-xs text-mnema-tertiary" />
    </button>
  </div>
</template>
