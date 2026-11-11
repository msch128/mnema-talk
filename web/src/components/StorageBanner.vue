<script setup lang="ts">
// Shown under the header while the server reports its object storage as down:
// uploads, attachments and pictures fail then, chat and Talks keep working.
import { HardDrive } from '@lucide/vue'
import { useServerFeaturesStore } from '../stores/serverFeatures'

const serverFeatures = useServerFeaturesStore()
</script>

<template>
  <div
    v-if="serverFeatures.storageDown"
    role="status"
    data-testid="storage-banner"
    class="flex min-h-10 shrink-0 items-center gap-2.5 border-b border-mnema-warning/35 bg-mnema-warning/10 py-1.5 pl-4 pr-3 text-sm text-mnema-text"
  >
    <HardDrive class="h-4 w-4 shrink-0 text-mnema-warning" />
    <span class="min-w-0 flex-1">{{ $t('connection.storageDown') }}</span>
  </div>
</template>
