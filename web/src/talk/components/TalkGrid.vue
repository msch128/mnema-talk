<script setup lang="ts">
import { talkTileProps, type TalkTileEmits } from './talkTileProps'
import { computed, ref } from 'vue'
import { useVideoGrid } from '../composables/useVideoGrid'
import TalkTile from './TalkTile.vue'

// The grid: every participant as a 16:9 tile, as large as the area allows,
// like Discord. Tiles without any camera stay smaller: a huge avatar tile
// only looks empty.
const props = defineProps({
  ...talkTileProps,
  connected: { type: Boolean, default: false }
})
const emit = defineEmits<TalkTileEmits>()

const gridArea = ref<HTMLElement | null>(null)
const GRID_GAP = 12
const hasVideo = computed(() => props.connected && props.users.some(u => !!props.streamOf(u)))
const { layout, gridStyle, tileStyle } = useVideoGrid(gridArea, {
  count: () => props.users.length,
  gap: GRID_GAP,
  maxTileWidth: () => (hasVideo.value ? 1280 : 640),
  minTileWidth: 160
})
const sized = computed(() => layout.value.tileWidth > 0)
</script>

<template>
  <div
    ref="gridArea"
    data-testid="talk-grid"
    :data-grid-cols="layout.cols"
    :class="[
      'w-full flex-1 min-h-0 flex justify-center',
      layout.overflow ? 'items-start overflow-y-auto' : 'items-center'
    ]"
  >
    <div
      :class="['flex flex-wrap justify-center content-center', sized ? '' : 'w-full max-w-5xl gap-3']"
      :style="gridStyle"
    >
      <TalkTile
        v-for="user in users"
        :key="user.id"
        :user="user"
        :stream="connected ? streamOf(user) : null"
        :camera-focusable="canFocusCamera(user)"
        :camera-focused="isCameraFocused(user)"
        fill
        :compact="sized && layout.tileWidth < 300"
        :show-status="connected"
        :style="tileStyle"
        :class="sized ? 'shrink-0' : 'w-56'"
        @focus-camera="emit('focus-camera', user)"
        @fullscreen-camera="emit('fullscreen-camera', user.id)"
        @watch-stream="emit('watch-stream', user.id)"
        @menu="emit('menu', $event, user)"
      />
    </div>
  </div>
</template>
