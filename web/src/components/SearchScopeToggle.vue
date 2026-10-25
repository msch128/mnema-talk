<script setup lang="ts">
import type { SearchScope } from '../lib/globalSearch'

// Channels or direct messages: what the search looks through.
defineProps<{ scope?: SearchScope | undefined }>()
const emit = defineEmits<{ 'update:scope': [scope: SearchScope] }>()
</script>

<template>
  <div role="group" :aria-label="$t('dm.searchScope')" class="flex shrink-0 rounded-full border border-mnema-border p-0.5" data-testid="search-scope">
    <button
      v-for="s in (['channels', 'dm'] as const)"
      :key="s"
      type="button"
      :aria-pressed="scope === s ? 'true' : 'false'"
      :data-scope="s"
      :class="['rounded-full px-2.5 py-0.5 text-xs font-medium transition max-md:min-h-8', scope === s ? 'bg-mnema-accent/15 text-mnema-accent' : 'text-mnema-muted hover:text-mnema-text']"
      @mousedown.prevent
      @click="emit('update:scope', s)"
    >{{ $t(s === 'dm' ? 'dm.section' : 'sidebar.channels') }}</button>
  </div>
</template>
