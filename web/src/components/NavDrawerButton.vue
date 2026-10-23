<script setup lang="ts">
import { Menu } from '@lucide/vue'
import { useCompactLayout } from '../composables/useCompactLayout'

// Phones only: opens the channel list, which is a drawer there.
const { openNav } = useCompactLayout()
</script>

<template>
  <button
    type="button"
    data-testid="open-nav"
    class="hidden max-md:flex h-11 w-11 flex-shrink-0 items-center justify-center rounded-md text-mnema-tertiary transition hover:bg-mnema-surface hover:text-mnema-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent"
    :aria-label="$t('sidebar.openChannels')"
    @click="openNav"
  >
    <Menu class="h-5 w-5" />
  </button>
</template>
