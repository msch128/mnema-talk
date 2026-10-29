<script setup lang="ts">
import { Plus, Save } from '@lucide/vue'

// The layout editor's toolbar: a new category and saving the order.
defineProps({
  saving: { type: Boolean, default: false },
  creating: { type: Boolean, default: false },
  changed: { type: Boolean, default: false }
})
const emit = defineEmits<{ create: []; save: [] }>()
const newCategoryName = defineModel<string>('newCategoryName', { required: true })
</script>

<template>
  <div class="flex w-full min-w-0 flex-wrap items-center gap-2 md:ml-auto md:w-auto">
    <!-- Add Category form -->
    <div class="flex min-w-0 items-center gap-1.5 max-md:basis-full md:flex-none">
      <input
        v-model="newCategoryName"
        type="text"
        :placeholder="$t('admin.categoryNamePlaceholder')"
        :aria-label="$t('admin.categoryNameLabel')"
        class="bg-mnema-surface border border-mnema-border-field rounded-md px-2.5 py-1 text-sm text-mnema-text placeholder-mnema-tertiary outline-hidden focus:border-mnema-accent transition min-w-0 flex-1 md:w-44 md:flex-none"
        @keydown.enter="!$event.isComposing && emit('create')"
      />
      <button
        type="button"
        :disabled="creating || !newCategoryName.trim()"
        class="btn btn-secondary btn-sm flex shrink-0 items-center gap-1"
        @click="emit('create')"
      >
        <Plus class="w-4 h-4" />
        <span>{{ $t('admin.addCategory') }}</span>
      </button>
    </div>

    <!-- Save Layout button -->
    <button
      data-testid="save-layout-button"
      type="button"
      :disabled="saving"
      :class="[
        'flex shrink-0 items-center justify-center gap-1.5 px-3 py-1 rounded-md text-sm font-semibold transition shadow-xs',
        changed
          ? 'bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover ring-2 ring-mnema-accent/40'
          : 'bg-mnema-elevated border border-mnema-border text-mnema-muted hover:text-mnema-text'
      ]"
      @click="emit('save')"
    >
      <Save class="w-4 h-4" />
      <span>{{ saving ? $t('common.save') : $t('admin.saveLayout') }}</span>
    </button>
  </div>
</template>
