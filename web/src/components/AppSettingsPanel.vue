<script setup lang="ts">
// "Push & App": install Mnema Talk on this device and get notified while it is closed.
import AppInstallSection from './AppInstallSection.vue'
import PushSection from './PushSection.vue'
</script>

<template>
  <div class="space-y-6">
    <AppInstallSection />
    <PushSection />
  </div>
</template>
