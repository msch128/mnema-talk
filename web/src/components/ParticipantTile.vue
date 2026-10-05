<script setup>
// One participant of the Talk: their camera when it is on, else their avatar.
// A click on a camera puts it on the stage (again: takes it off), a click on
// an avatar opens the profile; the context menu has the profile for both.
import { ref, computed, watch, nextTick } from 'vue'
import { MicOff, Eye, EyeOff, Monitor, X } from '@lucide/vue'
import UserAvatar from './UserAvatar.vue'
import VoiceTimer from './VoiceTimer.vue'
import MuteMarks from './MuteMarks.vue'

const props = defineProps({
  user: { type: Object, required: true },
  // They muted their microphone / deafened themselves (shown to everyone).
  muted: { type: Boolean, default: false },
  deafened: { type: Boolean, default: false },
  // Camera stream of this participant, null while the camera is off.
  stream: { type: Object, default: null },
  isSelf: { type: Boolean, default: false },
  speaking: { type: Boolean, default: false },
  compact: { type: Boolean, default: false },
  localMuted: { type: Boolean, default: false },
  // Preview: nobody to hear yet, so no speaking/ready status line.
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
const emit = defineEmits(['open-profile', 'toggle-camera', 'focus-camera', 'watch-stream', 'stop-watching', 'menu'])

const focusable = computed(() => !!props.stream && (props.cameraFocusable || props.cameraFocused))
const name = computed(() => props.user.display_name || props.user.username)

function handleTileClick() {
  if (focusable.value) {
    emit('focus-camera', props.user.id)
    return
  }
  if (props.isScreensharing && !props.isSelf) {
    emit('watch-stream', props.user.id)
    return
  }
  emit('open-profile', props.user)
}

const videoEl = ref(null)
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
    :aria-label="focusable ? (cameraFocused ? $t('talk.unfocusCamera') : $t('talk.focusCamera', { name })) : $t('profile.open', { name })"
    :aria-pressed="focusable ? (cameraFocused ? 'true' : 'false') : undefined"
    @click="handleTileClick"
    @contextmenu.prevent="emit('menu', $event)"
    @keydown.f10.shift.self.prevent="emit('menu', $event)"
    @keydown.context-menu.self.prevent="emit('menu', $event)"
    aria-haspopup="menu"
    @keydown.enter.self.prevent="handleTileClick"
    @keydown.space.self.prevent="handleTileClick"
    :class="[
      'relative cursor-pointer rounded-xl border overflow-hidden flex flex-col items-center justify-center transition-all shadow-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent group',
      stream ? 'bg-black aspect-video' : (compact ? 'p-3 h-28 bg-mnema-surface/90' : 'p-6 h-52 bg-mnema-surface'),
      speaking
        ? 'border-mnema-accent ring-2 ring-mnema-accent/40 shadow-lg shadow-mnema-accent/10'
        : cameraFocused
          ? 'border-mnema-accent'
          : 'border-mnema-hairline hover:border-mnema-border'
    ]"
  >
    <!-- LIVE Badge -->
    <div
      v-if="isScreensharing"
      data-testid="tile-live-badge"
      class="absolute top-2 left-2 z-10 px-1.5 py-0.5 rounded bg-red-600 text-white text-[10px] font-black uppercase tracking-wider flex items-center gap-1 shadow-md shadow-red-900/40"
    >
      <span class="w-1.5 h-1.5 rounded-full bg-white animate-pulse"></span>
      <span>{{ $t('talk.live') }}</span>
    </div>

    <!-- Watch Stream / Stop Watching Overlay on Hover -->
    <div
      v-if="isScreensharing && !isSelf"
      class="absolute inset-0 bg-black/60 opacity-0 group-hover:opacity-100 transition-opacity flex flex-col items-center justify-center gap-2 p-2 z-20 pointer-events-none"
    >
      <button
        v-if="!isWatching"
        type="button"
        data-testid="tile-watch-button"
        class="pointer-events-auto px-3 py-1.5 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold text-xs flex items-center gap-1.5 shadow-lg transition transform active:scale-95 disabled:opacity-60"
        :disabled="isConnecting"
        @click.stop="$emit('watch-stream', user.id)"
      >
        <Monitor class="w-3.5 h-3.5" />
        <span>{{ isConnecting ? $t('talk.screenConnecting') : $t('talk.watchStream') }}</span>
      </button>
      <button
        v-else
        type="button"
        data-testid="tile-stop-watching-button"
        class="pointer-events-auto px-3 py-1.5 rounded-md bg-mnema-surface border border-mnema-border text-white hover:bg-mnema-hover font-semibold text-xs flex items-center gap-1.5 shadow-lg transition"
        @click.stop="$emit('stop-watching', user.id)"
      >
        <X class="w-3.5 h-3.5" />
        <span>{{ $t('talk.stopWatching') }}</span>
      </button>
    </div>
    <!-- Camera -->
    <video
      v-if="stream"
      ref="videoEl"
      autoplay
      playsinline
      muted
      :aria-label="$t('talk.cameraOf', { name: user.display_name || user.username })"
      :class="['absolute inset-0 w-full h-full object-cover', isSelf ? '-scale-x-100' : '']"
    ></video>

    <!-- Avatar -->
    <template v-else>
      <div class="relative mb-2">
        <UserAvatar
          :user="user"
          :size="compact ? 'lg' : 'xl'"
          :is-speaking="speaking"
          class="hover:opacity-90 transition"
        />
        <span
          v-if="muted || deafened"
          class="absolute -bottom-0.5 -right-0.5 flex items-center justify-center rounded-full bg-mnema-surface p-1 shadow"
        >
          <MuteMarks :muted="muted" :deafened="deafened" :size="compact ? 12 : 14" />
        </span>
      </div>
    </template>

    <!-- Hide or show their camera (only for me) -->
    <button
      v-if="cameraAvailable && !isSelf"
      type="button"
      :class="[
        'absolute top-2 right-2 w-7 h-7 flex items-center justify-center rounded-md bg-black/60 text-white hover:bg-black/80 transition focus:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent',
        cameraHidden ? '' : 'opacity-0 hover:opacity-100 focus-visible:opacity-100'
      ]"
      v-tooltip="cameraHidden ? $t('talk.showCamera') : $t('talk.hideCamera')"
      @click.stop="$emit('toggle-camera')"
    >
      <Eye v-if="cameraHidden" class="w-4 h-4" />
      <EyeOff v-else class="w-4 h-4" />
    </button>

    <!-- Name, centered under the avatar. On camera: a pill centered at the
         bottom with the time next to it. Long names are cut with an ellipsis. -->
    <div
      data-tile-name
      :class="[
        'flex items-center justify-center gap-1 min-w-0',
        stream ? 'absolute inset-x-2 bottom-2' : 'w-full'
      ]"
    >
      <div
        :class="[
          'flex items-center justify-center gap-1.5 min-w-0',
          stream ? 'bg-black/70 rounded-md px-2 py-0.5 text-white' : 'max-w-[90%]'
        ]"
      >
        <span
          class="text-sm font-semibold hover:text-mnema-accent transition truncate"
          :class="stream ? 'text-white' : 'text-mnema-text'"
        >
          {{ user.display_name || user.username }}
        </span>
        <span v-if="user.role === 'admin' && !stream" class="text-xs px-1 rounded bg-amber-500/10 text-amber-400 font-mono flex-shrink-0">
          {{ $t('role.admin') }}
        </span>
        <MicOff v-if="localMuted" class="w-3.5 h-3.5 text-mnema-danger flex-shrink-0" v-tooltip="$t('talk.localMuted')" />
      </div>
      <VoiceTimer
        v-if="stream && user.joined_at"
        :since="user.joined_at"
        class="flex-shrink-0 rounded-md bg-black/70 px-1.5 py-0.5 text-xs text-white"
      />
    </div>

    <!-- Status: how long they have been in the Talk (speaking shows as the green ring) -->
    <div v-if="showStatus && !stream" class="w-full text-center text-xs font-mono mt-0.5" data-tile-status>
      <VoiceTimer v-if="user.joined_at" :since="user.joined_at" class="text-mnema-tertiary" />
      <span v-else class="text-mnema-tertiary">{{ $t('talk.ready') }}</span>
    </div>
  </div>
</template>
