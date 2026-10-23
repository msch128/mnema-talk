<script setup lang="ts">
import { talkTileProps, type TalkTileEmits } from './talkTileProps'
import { ref } from 'vue'
import { ChevronDown, ChevronUp, Users } from '@lucide/vue'
import { loadStripCollapsed, saveStripCollapsed } from '../lib/talkLayout'
import TalkTile from './TalkTile.vue'
import { focusRing } from './talkButtons'

// Participants under the stage: a strip that collapses (like Discord's
// "hide members", remembered per browser); the stage then takes the room.
defineProps(talkTileProps)
const emit = defineEmits<TalkTileEmits>()

const collapsed = ref(loadStripCollapsed())
function toggle() {
  collapsed.value = !collapsed.value
  saveStripCollapsed(collapsed.value)
}
</script>

<template>
  <button
    type="button"
    data-testid="talk-strip-toggle"
    :aria-expanded="collapsed ? 'false' : 'true'"
    aria-controls="talk-strip"
    v-tooltip="collapsed ? $t('talk.showParticipants') : $t('talk.hideParticipants')"
    :class="['h-6 px-2 flex items-center gap-1 rounded-full text-xs font-semibold text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-hover transition-colors motion-reduce:transition-none', focusRing]"
    @click="toggle"
  >
    <ChevronUp v-if="collapsed" class="w-4 h-4" aria-hidden="true" />
    <ChevronDown v-else class="w-4 h-4" aria-hidden="true" />
    <template v-if="collapsed">
      <Users class="w-3.5 h-3.5" aria-hidden="true" />
      <span>{{ users.length }}</span>
    </template>
  </button>
  <div
    :class="['w-full grid transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none', collapsed ? 'grid-rows-[0fr]' : 'grid-rows-[1fr]']"
  >
    <div class="min-h-0 overflow-hidden">
      <div
        id="talk-strip"
        data-testid="talk-strip"
        :data-collapsed="collapsed ? 'true' : 'false'"
        :inert="collapsed"
        class="w-full overflow-x-auto pt-1.5 pb-1"
      >
        <div class="flex gap-2 w-max mx-auto px-0.5">
          <TalkTile
            v-for="user in users"
            :key="user.id"
            :user="user"
            :stream="streamOf(user)"
            :camera-focusable="canFocusCamera(user)"
            :camera-focused="isCameraFocused(user)"
            compact
            class="shrink-0"
            @focus-camera="emit('focus-camera', user)"
            @fullscreen-camera="emit('fullscreen-camera', user.id)"
            @watch-stream="emit('watch-stream', user.id)"
            @menu="emit('menu', $event, user)"
          />
        </div>
      </div>
    </div>
  </div>
</template>
