<script setup lang="ts">
import { viewerState } from '../composables/useMediaViewer'
import MediaViewerDialog from './MediaViewerDialog.vue'

// Mounted once in App.vue. The dialog exists only while the viewer is open:
// its focus trap, initial focus and Escape belong to a dialog that appears.
</script>

<template>
  <MediaViewerDialog v-if="viewerState.open && viewerState.items.length" />
</template>
