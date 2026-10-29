<script setup lang="ts">
// The footer of every explicit-save screen (see useSettingsDraft): says when
// there are unsaved changes, offers Reset and Save, and shows a failed save
// inline. Save is on only when the draft differs and is valid.
import { saveShortcutLabel } from '../composables/useSettingsDraft'

defineProps({
  dirty: { type: Boolean, default: false },
  canSave: { type: Boolean, default: false },
  saving: { type: Boolean, default: false },
  error: { type: String, default: '' },
  /** Replaces "Save" where the form creates something. */
  saveLabel: { type: String, default: '' },
  saveTestid: { type: String, default: 'unsaved-save' },
  /** Stays at the bottom of an admin page (inside the dashboard's padded area) instead of sitting in a dialog footer. */
  sticky: { type: Boolean, default: false }
})
const emit = defineEmits<{ save: []; reset: [] }>()
</script>

<template>
  <div :class="['shrink-0 border-t border-mnema-hairline bg-mnema-raised', sticky && 'sticky bottom-0 z-10 -mx-6 -mb-6 max-md:-mx-4 max-md:-mb-4']" data-testid="unsaved-bar">
    <p v-if="error" role="alert" class="px-5 pt-3 text-sm text-mnema-danger" data-testid="unsaved-error">{{ error }}</p>
    <div class="flex min-h-14 items-center justify-between gap-3 px-5 py-3 max-md:flex-wrap">
      <p class="flex min-w-0 items-center gap-2 text-sm text-mnema-muted" role="status" data-testid="unsaved-status">
        <template v-if="dirty">
          <span class="h-2 w-2 shrink-0 rounded-full bg-mnema-accent" aria-hidden="true" />
          <span class="leading-snug">{{ $t('draft.unsaved') }}</span>
        </template>
      </p>
      <div class="flex shrink-0 items-center gap-2">
        <button v-if="dirty" type="button" class="btn btn-ghost h-9" data-testid="unsaved-reset" :disabled="saving" @click="emit('reset')">{{ $t('draft.reset') }}</button>
        <button
          type="button"
          class="btn btn-primary h-9"
          :data-testid="saveTestid"
          :disabled="!canSave"
          :aria-busy="saving ? 'true' : undefined"
          :title="$t('draft.shortcut', { keys: saveShortcutLabel })"
          @click="emit('save')"
        >{{ saving ? $t('draft.saving') : saveLabel || $t('common.save') }}</button>
      </div>
    </div>
  </div>
</template>
