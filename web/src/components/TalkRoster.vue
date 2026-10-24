<script setup lang="ts">
import { talkTileProps } from './talkTileProps'
import { computed } from 'vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { isStageTalk } from '../lib/talks'
import StageLayout from './StageLayout.vue'
import TalkGrid from './TalkGrid.vue'

// The participants of a Talk: the tile grid, or the speakers and audience of a
// Stage. Events of the tiles (focus, full screen, watch, menu) pass through.
const props = defineProps({
  ...talkTileProps,
  channelId: { type: String, default: '' },
  connected: { type: Boolean, default: false }
})

const features = useServerFeaturesStore()
const chat = useChatStore()
const isStage = computed(() => features.talksStage && !!props.channelId && isStageTalk(chat.allChannels.find(c => c.id === props.channelId)))
</script>

<template>
  <StageLayout v-if="isStage" v-bind="props" />
  <TalkGrid v-else v-bind="props" />
</template>
