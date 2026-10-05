<script setup>
// One participant of the roundtable: their camera when it is on, else their avatar.
import { ref, watch, nextTick } from 'vue'
import { MicOff } from '@lucide/vue'
import UserAvatar from './UserAvatar.vue'

const props = defineProps({
  user: { type: Object, required: true },
  // Camera stream of this participant, null while the camera is off.
  stream: { type: Object, default: null },
  isSelf: { type: Boolean, default: false },
  speaking: { type: Boolean, default: false },
  compact: { type: Boolean, default: false },
  localMuted: { type: Boolean, default: false },
  // Preview: nobody to hear yet, so no speaking/ready status line.
  showStatus: { type: Boolean, default: true }
})
defineEmits(['open-profile'])

const videoEl = ref(null)
watch(() => props.stream, (stream) => {
  nextTick(() => {
    if (videoEl.value) videoEl.value.srcObject = stream
  })
}, { immediate: true, flush: 'post' })
</script>

<template>
  <div
    :class="[
      'relative rounded-xl border overflow-hidden flex flex-col items-center justify-center transition-all shadow-sm',
      stream ? 'bg-black aspect-video' : (compact ? 'p-3 h-28 bg-mnema-surface/90' : 'p-6 h-52 bg-mnema-surface'),
      speaking
        ? 'border-mnema-accent ring-2 ring-mnema-accent/40 shadow-lg shadow-mnema-accent/10'
        : 'border-mnema-hairline hover:border-mnema-border'
    ]"
  >
    <!-- Camera -->
    <video
      v-if="stream"
      ref="videoEl"
      autoplay
      playsinline
      muted
      :aria-label="$t('tafelrunde.cameraOf', { name: user.display_name || user.username })"
      :class="['absolute inset-0 w-full h-full object-cover', isSelf ? '-scale-x-100' : '']"
    ></video>

    <!-- Avatar -->
    <template v-else>
      <div class="relative mb-2">
        <UserAvatar
          :user="user"
          :size="compact ? 'lg' : 'xl'"
          :is-speaking="speaking"
          class="cursor-pointer hover:opacity-90 transition"
          @click="$emit('open-profile', user)"
        />
      </div>
    </template>

    <!-- Name -->
    <div
      :class="[
        'flex items-center gap-1.5 max-w-[90%]',
        stream ? 'absolute left-2 bottom-2 bg-black/70 rounded-md px-2 py-0.5 text-white' : ''
      ]"
    >
      <span
        class="text-sm font-semibold hover:text-mnema-accent transition cursor-pointer truncate"
        :class="stream ? 'text-white' : 'text-mnema-text'"
        @click="$emit('open-profile', user)"
      >
        {{ user.display_name || user.username }}
      </span>
      <span v-if="user.role === 'admin' && !stream" class="text-xs px-1 rounded bg-amber-500/10 text-amber-400 font-mono flex-shrink-0">
        {{ $t('role.admin') }}
      </span>
      <MicOff v-if="localMuted" class="w-3.5 h-3.5 text-mnema-danger flex-shrink-0" v-tooltip="$t('tafelrunde.localMuted')" />
    </div>

    <!-- Status -->
    <div v-if="showStatus && !stream" class="text-xs font-mono mt-0.5">
      <span v-if="speaking" class="text-mnema-accent font-semibold flex items-center gap-1">
        <span class="w-1.5 h-1.5 rounded-full bg-mnema-accent shadow-[0_0_4px_rgba(45,167,113,0.8)]"></span>
        {{ $t('tafelrunde.speaking') }}
      </span>
      <span v-else class="text-mnema-tertiary">{{ $t('tafelrunde.ready') }}</span>
    </div>
  </div>
</template>
