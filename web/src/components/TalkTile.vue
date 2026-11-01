<script setup lang="ts">
import type { PropType } from 'vue'
import type { TalkParticipant } from './presentationTypes'
import { computed } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import ParticipantTile from './ParticipantTile.vue'

// A participant's tile in the Talk (grid and strip alike): what the stores
// know about them goes to the tile; only the choices that need the stage
// (focus, full screen, watch) go up. Size and shape attributes fall through.
const props = defineProps({
  user: { type: Object as PropType<TalkParticipant>, required: true },
  stream: { type: Object as PropType<MediaStream | null>, default: null },
  cameraFocusable: { type: Boolean, default: false },
  cameraFocused: { type: Boolean, default: false }
})
const emit = defineEmits<{ 'focus-camera': []; 'fullscreen-camera': []; 'watch-stream': []; menu: [event: MouseEvent | KeyboardEvent] }>()

const voiceStore = useVoiceStore()
const chatStore = useChatStore()
const authStore = useAuthStore()

const reactions = computed(() => voiceStore.talkReactions.filter(r => r.user_id === props.user.id))

function cameraAvailable() {
  return !voiceStore.allCamerasOff && !!voiceStore.mediaState[props.user.id]?.camera
}
</script>

<template>
  <ParticipantTile
    :user="user"
    :stream="stream"
    :is-self="user.id === authStore.user?.id"
    :speaking="voiceStore.isSpeaking(user.id)"
    :muted="voiceStore.muteStateOf(user.id).muted"
    :deafened="voiceStore.muteStateOf(user.id).deafened"
    :local-muted="voiceStore.isUserLocalMuted(user.id)"
    :camera-available="cameraAvailable()"
    :camera-hidden="voiceStore.isCameraHidden(user.id)"
    :camera-capped="voiceStore.isCameraCapped(user.id)"
    :is-screensharing="!!voiceStore.mediaState[user.id]?.screen"
    :is-watching="!!voiceStore.watchedScreens[user.id]"
    :is-connecting="voiceStore.remoteScreenUserId === user.id && !voiceStore.remoteScreenStream"
    :camera-focusable="cameraFocusable"
    :camera-focused="cameraFocused"
    :reactions="reactions"
    @toggle-camera="voiceStore.toggleCameraHidden(user.id)"
    @focus-camera="emit('focus-camera')"
    @fullscreen-camera="emit('fullscreen-camera')"
    @watch-stream="emit('watch-stream')"
    @stop-watching="voiceStore.unwatchScreen(user.id)"
    @open-profile="chatStore.openUserProfile"
    @menu="emit('menu', $event)"
  />
</template>
