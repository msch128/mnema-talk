<script setup lang="ts">
import { talkTileProps, type TalkTileEmits } from './talkTileProps'
import { computed } from 'vue'
import { Hand, HandMetal, MicOff, UserMinus, UserPlus } from '@lucide/vue'
import { useStage } from '../composables/useStage'
import { splitStage } from '../lib/stage'
import { useAuthStore } from '../stores/auth'
import StageHandQueue from './StageHandQueue.vue'
import TalkTile from './TalkTile.vue'
import TalkReactionFloat from './TalkReactionFloat.vue'
import UserAvatar from './UserAvatar.vue'
import { useVoiceStore } from '../stores/voice'
import { focusRing } from './talkButtons'

// A Stage: the speakers in a row of tiles, the audience as a quiet list, and
// for those who manage the Stage the raised hands to answer. Members without
// talk power ask with "Raise hand". The server refuses audio and video from
// the audience regardless of what is shown here.
const props = defineProps({
  ...talkTileProps,
  channelId: { type: String, required: true },
  connected: { type: Boolean, default: false }
})
const emit = defineEmits<TalkTileEmits>()

const me = computed(() => useAuthStore().user?.id ?? null)
const stage = useStage(() => props.channelId)
const voice = useVoiceStore()
const reactionsOf = (userId: string) => voice.talkReactions.filter(r => r.user_id === userId)

// The server's talk_power for the seats, plus what I know of myself.
const seats = computed(() => props.users.map(u => (u.id === me.value && stage.iAmSpeaker.value ? { ...u, talk_power: true } : u)))
const split = computed(() => splitStage(seats.value))
const showHandButton = computed(() => props.connected && !stage.iAmSpeaker.value)
// Speaker tiles use the canvas: one speaker is large, more share the width.
const speakerWidth = computed(() => {
  const n = split.value.speakers.length
  if (n <= 1) return 'w-[min(100%,30rem)]'
  if (n === 2) return 'w-[min(calc(50%-0.375rem),22.5rem)]'
  return n <= 4 ? 'w-[min(calc(50%-0.375rem),17.5rem)]' : 'w-[min(calc(50%-0.375rem),14rem)]'
})
const name = (user: { display_name?: string | undefined; username?: string | undefined }) => user.display_name || user.username || ''
</script>

<template>
  <div data-testid="stage-layout" class="flex w-full max-w-5xl flex-1 min-h-0 flex-col gap-4 overflow-y-auto">
    <section :aria-label="$t('stage.speakers')" class="shrink-0">
      <h2 class="mb-2 text-xs font-semibold uppercase tracking-wider text-mnema-tertiary">{{ $t('stage.speakers') }}</h2>
      <div v-if="split.speakers.length" data-testid="stage-speakers" class="flex flex-wrap justify-center gap-3">
        <div v-for="user in split.speakers" :key="user.id" :class="[speakerWidth, 'flex shrink-0 flex-col gap-1']">
          <TalkTile
            :user="user"
            :stream="connected ? streamOf(user) : null"
            :camera-focusable="canFocusCamera(user)"
            :camera-focused="isCameraFocused(user)"
            fill
            :show-status="connected"
            class="w-full"
            @focus-camera="emit('focus-camera', user)"
            @fullscreen-camera="emit('fullscreen-camera', user.id)"
            @watch-stream="emit('watch-stream', user.id)"
            @menu="emit('menu', $event, user)"
          />
          <button
            v-if="stage.canManage.value && user.id !== me"
            type="button"
            data-testid="stage-take"
            :class="['inline-flex h-7 items-center justify-center gap-1 self-center rounded-full px-2.5 text-xs text-mnema-tertiary transition-colors hover:bg-mnema-hover hover:text-mnema-text motion-reduce:transition-none', focusRing]"
            @click="stage.takeTalkPower(user.id)"
          >
            <UserMinus class="h-3.5 w-3.5" aria-hidden="true" />
            <span>{{ $t('stage.take', { name: name(user) }) }}</span>
          </button>
        </div>
      </div>
      <p v-else data-testid="stage-nobody" class="rounded-lg border border-dashed border-mnema-border px-4 py-6 text-center text-sm text-mnema-tertiary">{{ $t('stage.nobodySpeaking') }}</p>
    </section>

    <StageHandQueue
      v-if="stage.canManage.value"
      :hands="split.hands"
      @accept="stage.giveTalkPower"
      @dismiss="stage.dismissHand"
    />

    <section :aria-label="$t('stage.audience')" class="shrink-0">
      <div class="mb-2 flex items-center justify-between gap-2">
        <h2 class="text-xs font-semibold uppercase tracking-wider text-mnema-tertiary">
          {{ $t('stage.audience') }} <span class="font-mono" data-testid="stage-audience-count">{{ split.audience.length }}</span>
        </h2>
        <button
          v-if="showHandButton"
          type="button"
          data-testid="stage-raise-hand"
          :aria-pressed="stage.myHandRaised.value ? 'true' : 'false'"
          :class="[
            'inline-flex h-9 items-center gap-1.5 rounded-full px-4 text-sm font-semibold transition-colors motion-reduce:transition-none',
            stage.myHandRaised.value ? 'bg-mnema-accent-subtle text-mnema-accent ring-1 ring-inset ring-mnema-accent/30' : 'bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover',
            focusRing
          ]"
          @click="stage.myHandRaised.value ? stage.lowerHand() : stage.raiseHand()"
        >
          <HandMetal v-if="stage.myHandRaised.value" class="h-4 w-4" aria-hidden="true" />
          <Hand v-else class="h-4 w-4" aria-hidden="true" />
          <span>{{ stage.myHandRaised.value ? $t('stage.lowerHand') : $t('stage.raiseHand') }}</span>
        </button>
      </div>
      <ul v-if="split.audience.length" data-testid="stage-audience" class="grid gap-1 sm:grid-cols-2">
        <li
          v-for="user in split.audience"
          :key="user.id"
          data-testid="stage-listener"
          tabindex="0"
          aria-haspopup="menu"
          :class="['relative flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-mnema-hover', focusRing]"
          @contextmenu.prevent="emit('menu', $event, user)"
          @keydown.f10.shift.self.prevent="emit('menu', $event, user)"
          @keydown.context-menu.self.prevent="emit('menu', $event, user)"
        >
          <TalkReactionFloat :reactions="reactionsOf(user.id)" compact />
          <UserAvatar :user="user" size="xs" />
          <span class="min-w-0 flex-1 truncate text-sm text-mnema-text">{{ name(user) }}</span>
          <span v-if="user.hand_raised_at" class="inline-flex shrink-0 items-center text-mnema-accent" data-testid="stage-listener-hand" v-tooltip="$t('stage.handRaised')">
            <Hand class="h-4 w-4" aria-hidden="true" />
            <span class="sr-only">{{ $t('stage.handRaised') }}</span>
          </span>
          <MicOff class="h-3.5 w-3.5 shrink-0 text-mnema-tertiary" role="img" :aria-label="$t('stage.listening')" v-tooltip="$t('stage.listening')" />
          <button
            v-if="stage.canManage.value && user.id !== me"
            type="button"
            data-testid="stage-give"
            :aria-label="$t('stage.give', { name: name(user) })"
            v-tooltip="$t('stage.give', { name: name(user) })"
            :class="['inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-mnema-tertiary transition-colors hover:bg-mnema-surface hover:text-mnema-text motion-reduce:transition-none', focusRing]"
            @click="stage.giveTalkPower(user.id)"
          >
            <UserPlus class="h-4 w-4" aria-hidden="true" />
          </button>
        </li>
      </ul>
      <p v-else class="px-1 text-sm text-mnema-tertiary">{{ $t('stage.noAudience') }}</p>
    </section>
  </div>
</template>
