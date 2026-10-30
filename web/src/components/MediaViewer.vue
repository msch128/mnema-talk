<script setup lang="ts">
import { computed } from 'vue'
import { viewerState } from '../composables/useMediaViewer'
import MediaViewerDialog from './MediaViewerDialog.vue'

// Mounted once in App.vue. The dialog exists only while the viewer is open:
// its focus trap, initial focus and Escape belong to a dialog that appears.
const shown = computed(() => viewerState.open && viewerState.items.length > 0)
</script>

<template>
  <MediaViewerDialog v-if="shown" />
</template>
