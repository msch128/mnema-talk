<script setup lang="ts">
import type { PropType } from 'vue'
import type { AvatarUser } from './presentationTypes'
// One participant of the Talk, like a Discord voice tile: their camera when it
// is on, else their avatar centered in the tile and scaled to its size. The
// name sits centered at the bottom, LIVE in the top-right corner, the time in
// the Talk only on hover or keyboard focus.
// A click on a camera puts it on the stage (again: takes it off), a click on
// an avatar opens the profile; the context menu has the profile for both.
import { ref, computed, watch, nextTick } from 'vue'
import { MicOff, Eye, EyeOff, Monitor, X } from '@lucide/vue'
import VoiceTimer from './VoiceTimer.vue'
import MuteMarks from './MuteMarks.vue'

const props = defineProps({
  user: { type: Object as PropType<AvatarUser & { id: string; joined_at?: string; role?: 'admin' | 'user' }>, required: true },
  // They muted their microphone / deafened themselves (shown to everyone).
  muted: { type: Boolean, default: false },
  deafened: { type: Boolean, default: false },
  // Camera stream of this participant, null while the camera is off.
  stream: { type: Object as PropType<MediaStream | null>, default: null },
  isSelf: { type: Boolean, default: false },
  speaking: { type: Boolean, default: false },
  // Smaller type and badges (the strip under a screen share, small grids).
  compact: { type: Boolean, default: false },
  // A tile of the grid: always 16:9, sized by the grid (style) rather than
  // by its content.
  fill: { type: Boolean, default: false },
  localMuted: { type: Boolean, default: false },
  // Preview: nobody to hear yet, so no time in the Talk.
  showStatus: { type: Boolean, default: true },
  // Their camera is running (known without receiving it) and I may hide it.
  cameraAvailable: { type: Boolean, default: false },
  cameraHidden: { type: Boolean, default: false },
  // Their camera can go on the stage / is on the stage.
  cameraFocusable: { type: Boolean, default: false },
  cameraFocused: { type: Boolean, default: false },
  // Screen sharing state
  isScreensharing: { type: Boolean, default: false },
  isWatching: { type: Boolean, default: false },
  isConnecting: { type: Boolean, default: false }
})
const emit = defineEmits<{ 'open-profile': [user: { id: string }]; 'toggle-camera': []; 'focus-camera': [id: string]; 'fullscreen-camera': [id: string]; 'watch-stream': [id: string]; 'stop-watching': [id: string]; menu: [event: MouseEvent | KeyboardEvent] }>()

const focusable = computed(() => !!props.stream && (props.cameraFocusable || props.cameraFocused))
const name = computed(() => props.user.display_name || props.user.username)
const initial = computed(() => (name.value || '?').charAt(0).toUpperCase())

// Shown on hover / keyboard focus of the tile (or of a button on it).
const REVEAL = 'opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100 group-has-[:focus-visible]:opacity-100'

function handleTileClick(e: MouseEvent | KeyboardEvent) {
  if (focusable.value) {
    // The second click of a double-click: the dblclick (fullscreen) decides.
    // The first one already acted, without waiting to tell them apart.
    if (e instanceof MouseEvent && e.detail >= 2) return
    emit('focus-camera', props.user.id)
    return
  }
  if (props.isScreensharing && !props.isSelf) {
    emit('watch-stream', props.user.id)
    return
  }
  emit('open-profile', props.user)
}

// A double-click on a camera: on the stage and full screen.
function onDblclick(e: MouseEvent) {
  if (!props.stream || !focusable.value || (e.target instanceof Element && e.target.closest('button'))) return
  e.stopPropagation()
  emit('fullscreen-camera', props.user.id)
}

const videoEl = ref<HTMLVideoElement | null>(null)
watch(() => props.stream, (stream) => {
  nextTick(() => {
    if (videoEl.value) videoEl.value.srcObject = stream
  })
}, { immediate: true, flush: 'post' })
</script>

<template>
  <div
    role="button"
    tabindex="0"
    data-participant-tile
    :data-user-id="user.id"
    :aria-label="focusable ? (cameraFocused ? $t('talk.unfocusCamera') : $t('talk.focusCamera', { name })) : $t('profile.open', { name })"
    :aria-pressed="focusable ? (cameraFocused ? 'true' : 'false') : undefined"
    @click="handleTileClick"
    @dblclick="onDblclick"
    @contextmenu.prevent="emit('menu', $event)"
    @keydown.f10.shift.self.prevent="emit('menu', $event)"
    @keydown.context-menu.self.prevent="emit('menu', $event)"
    aria-haspopup="menu"
    @keydown.enter.self.prevent="handleTileClick"
    @keydown.space.self.prevent="handleTileClick"
    :class="[
      'group relative isolate aspect-video cursor-pointer select-none overflow-hidden rounded-lg transition-colors duration-150 focus:outline-none',
      compact ? 'tile-compact' : '',
      fill ? '' : (compact ? 'w-44' : 'w-72'),
      stream ? 'bg-black' : 'bg-mnema-surface hover:bg-mnema-hover'
    ]"
  >
    <!-- Camera: fills the tile -->
    <video
      v-if="stream"
      ref="videoEl"
      autoplay
      playsinline
      muted
      :aria-label="$t('talk.cameraOf', { name })"
      :class="['absolute inset-0 w-full h-full object-cover', isSelf ? '-scale-x-100' : '']"
    ></video>

    <!-- Avatar: centered both ways, as large as the tile allows without
         reaching the corner badges or the name (see the style below). -->
    <div v-else class="tile-stage absolute inset-0 flex items-center justify-center pointer-events-none">
      <div
        data-tile-avatar
        :class="[
          'tile-avatar flex-shrink-0 rounded-full overflow-hidden flex items-center justify-center font-semibold border transition-shadow duration-150',
          compact ? 'text-base' : 'text-2xl',
          user.avatar_url ? 'border-mnema-border bg-mnema-surface' : 'bg-mnema-band border-mnema-mint/30 text-mnema-mint',
          speaking
            ? (compact ? 'ring-2 ring-offset-1' : 'ring-[3px] ring-offset-2') + ' ring-mnema-accent ring-offset-mnema-surface group-hover:ring-offset-mnema-hover'
            : ''
        ]"
      >
        <img
          v-if="user.avatar_url"
          :src="user.avatar_url"
          alt=""
          draggable="false"
          class="w-full h-full object-cover"
          loading="lazy"
        />
        <span v-else class="tile-initial leading-none" aria-hidden="true">{{ initial }}</span>
      </div>
    </div>

    <!-- Streaming: hover (or keyboard focus) offers to watch / stop watching -->
    <div
      v-if="isScreensharing && !isSelf"
      data-tile-watch-overlay
      :class="['absolute inset-0 z-20 flex items-center justify-center bg-black/55 transition-opacity duration-150 pointer-events-none', REVEAL]"
    >
      <button
        v-if="!isWatching"
        type="button"
        data-testid="tile-watch-button"
        :class="[
          'pointer-events-auto rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold flex items-center gap-1.5 shadow-lg transition active:scale-95 disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-mnema-text',
          compact ? 'h-6 px-2 text-xs' : 'h-8 px-3 text-sm'
        ]"
        :disabled="isConnecting"
        @click.stop="$emit('watch-stream', user.id)"
      >
        <Monitor :class="compact ? 'w-3.5 h-3.5' : 'w-4 h-4'" aria-hidden="true" />
        <span>{{ isConnecting ? $t('talk.screenConnecting') : $t('talk.watchStream') }}</span>
      </button>
      <button
        v-else
        type="button"
        data-testid="tile-stop-watching-button"
        :class="[
          'pointer-events-auto rounded-md bg-mnema-surface border border-mnema-border text-mnema-text hover:bg-mnema-hover font-semibold flex items-center gap-1.5 shadow-lg transition focus:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent',
          compact ? 'h-6 px-2 text-xs' : 'h-8 px-3 text-sm'
        ]"
        @click.stop="$emit('stop-watching', user.id)"
      >
        <X :class="compact ? 'w-3.5 h-3.5' : 'w-4 h-4'" aria-hidden="true" />
        <span>{{ $t('talk.stopWatching') }}</span>
      </button>
    </div>

    <!-- Top left: how long they have been in the Talk, only on hover/focus -->
    <div
      v-if="showStatus"
      data-tile-status
      :class="[
        'absolute z-30 flex items-center rounded bg-black/65 text-white/90 font-mono text-xs leading-4 tabular-nums pointer-events-none transition-opacity duration-150',
        compact ? 'top-1.5 left-1.5 h-4 px-1' : 'top-2 left-2 h-5 px-1.5',
        REVEAL
      ]"
    >
      <VoiceTimer v-if="user.joined_at" :since="user.joined_at" />
      <span v-else class="font-sans">{{ $t('talk.ready') }}</span>
    </div>

    <!-- Top right: hide/show their camera (only for me), LIVE while sharing -->
    <div
      v-if="isScreensharing || (cameraAvailable && !isSelf)"
      data-tile-corner
      :class="['absolute z-30 flex items-center gap-1', compact ? 'top-1.5 right-1.5' : 'top-2 right-2']"
    >
      <button
        v-if="cameraAvailable && !isSelf"
        type="button"
        :class="[
          'flex items-center justify-center rounded bg-black/65 text-white hover:bg-black/85 transition focus:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent',
          compact ? 'w-5 h-5' : 'w-6 h-6',
          cameraHidden ? '' : 'opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100 focus-visible:opacity-100'
        ]"
        v-tooltip="cameraHidden ? $t('talk.showCamera') : $t('talk.hideCamera')"
        @click.stop="$emit('toggle-camera')"
      >
        <Eye v-if="cameraHidden" :class="compact ? 'w-3 h-3' : 'w-3.5 h-3.5'" />
        <EyeOff v-else :class="compact ? 'w-3 h-3' : 'w-3.5 h-3.5'" />
      </button>
      <div
        v-if="isScreensharing"
        data-testid="tile-live-badge"
        :class="[
          'flex items-center gap-1 rounded bg-red-600 text-white font-bold uppercase tracking-wider leading-none shadow-md shadow-black/30 pointer-events-none',
          compact ? 'h-4 px-1 text-[10px]' : 'h-5 px-1.5 text-[11px]'
        ]"
      >
        <Monitor :class="['opacity-80', compact ? 'w-2.5 h-2.5' : 'w-3 h-3']" aria-hidden="true" />
        <span>{{ $t('talk.live') }}</span>
      </div>
    </div>

    <!-- Bottom: name (cut with an ellipsis), Admin and mute marks, centered -->
    <div
      data-tile-name
      :class="[
        'absolute inset-x-0 z-30 flex justify-center min-w-0',
        compact ? 'bottom-1.5 px-1.5' : 'bottom-2 px-2'
      ]"
    >
      <div
        :class="[
          'flex items-center justify-center gap-1 min-w-0 max-w-full rounded-md text-mnema-text',
          stream ? 'bg-black/70' : 'bg-black/45',
          compact ? 'h-[18px] px-1.5' : 'h-6 px-2'
        ]"
      >
        <span :class="['truncate font-semibold', compact ? 'text-xs' : 'text-sm']">{{ name }}</span>
        <span
          v-if="user.role === 'admin'"
          class="flex-shrink-0 rounded px-1 bg-amber-500/15 text-amber-400 font-mono text-xs leading-4"
        >{{ $t('role.admin') }}</span>
        <MicOff
          v-if="localMuted"
          :class="['flex-shrink-0 text-mnema-danger', compact ? 'w-3 h-3' : 'w-3.5 h-3.5']"
          v-tooltip="$t('talk.localMuted')"
        />
        <MuteMarks :muted="muted" :deafened="deafened" :size="compact ? 12 : 14" />
      </div>
    </div>

    <!-- Frame on top of everything (also the camera): green glow while
         speaking, the accent while their camera is on the stage. -->
    <div
      data-tile-frame
      :class="[
        'absolute inset-0 z-40 rounded-[inherit] pointer-events-none transition-shadow duration-150',
        speaking
          ? 'ring-2 ring-inset ring-mnema-accent shadow-[inset_0_0_14px_rgba(45,167,113,0.35)]'
          : cameraFocused
            ? 'ring-2 ring-inset ring-mnema-accent/70'
            : 'ring-1 ring-inset ring-white/[0.06] group-hover:ring-white/[0.12]'
      ]"
    ></div>
    <!-- Keyboard focus ring, inside the tile so the strip never clips it -->
    <div
      class="absolute inset-0 z-50 rounded-[inherit] pointer-events-none hidden group-focus-visible:block ring-2 ring-inset ring-mnema-text"
      aria-hidden="true"
    ></div>
  </div>
</template>

<style scoped>
/* The avatar scales with the tile: centered, never larger than the room
   between the top badges (LIVE, time) and the name at the bottom, so nothing
   overlaps it in any size or aspect ratio. --band is that reserved height at
   the top and the bottom (badge/name + speaking ring + a small gap). */
.tile-stage {
  container-type: size;
  --band: 2.5rem;
}
.tile-compact .tile-stage {
  --band: 1.8125rem;
}
.tile-avatar {
  width: 4rem;
  height: 4rem;
}
.tile-compact .tile-avatar {
  width: 2.5rem;
  height: 2.5rem;
}
@supports (width: 1cqh) {
  .tile-avatar,
  .tile-compact .tile-avatar {
    width: clamp(1.25rem, min(100cqh - 2 * var(--band), 42cqh, 40cqw), 7.5rem);
    height: clamp(1.25rem, min(100cqh - 2 * var(--band), 42cqh, 40cqw), 7.5rem);
    container-type: size;
  }
  .tile-initial {
    font-size: 42cqh;
  }
}
</style>
