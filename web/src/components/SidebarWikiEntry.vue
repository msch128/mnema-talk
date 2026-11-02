<script setup lang="ts">
// The wiki's place in the sidebar, beside Events: shown when the server has a
// wiki and the member may read it.
import { BookOpen } from '@lucide/vue'
import { computed } from 'vue'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { usePermissionsStore } from '../stores/permissions'
import { openWiki } from '../lib/wikiNav'

const features = useServerFeaturesStore()
const permissions = usePermissionsStore()
const shown = computed(() => features.wiki && permissions.can('view_wiki'))
</script>

<template>
  <div v-if="shown" class="mb-1 shrink-0">
    <button
      type="button"
      class="group flex w-full cursor-pointer items-center gap-2 rounded-lg px-2.5 py-1.5 text-sm font-medium text-mnema-secondary transition-colors hover:bg-mnema-hover hover:text-mnema-text"
      data-testid="sidebar-wiki"
      :aria-label="$t('wiki.title')"
      @click="openWiki()"
    >
      <BookOpen class="h-4 w-4 shrink-0 text-mnema-tertiary group-hover:text-mnema-text" />
      <span class="truncate">{{ $t('wiki.title') }}</span>
    </button>
  </div>
</template>
