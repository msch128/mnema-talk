<script setup>
// Owns the <video> of the Picture-in-Picture window, outside the Talk view,
// so the window keeps playing in a text channel (see usePictureInPicture).
import { ref } from 'vue'
import { usePictureInPictureHost } from '../composables/usePictureInPicture'

const videoEl = ref(null)
usePictureInPictureHost(videoEl)
</script>

<template>
  <video
    ref="videoEl"
    data-testid="pip-video"
    muted
    playsinline
    aria-hidden="true"
    tabindex="-1"
    class="fixed bottom-0 right-0 w-px h-px opacity-0 pointer-events-none"
  ></video>
</template>
