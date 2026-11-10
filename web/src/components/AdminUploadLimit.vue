<script setup lang="ts">
// Owner only (System tab): the global upload limit and whether pictures lose
// their metadata. MAX_UPLOAD_SIZE_MB stays the operator's ceiling; this can
// only lower it. Roles may override it.
import { computed, ref, onMounted } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodePermUploadSettings } from '../types/rest'
import type { PermUploadSettings } from '../types/rest'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

const toasts = useToastStore()
const settings = ref<PermUploadSettings | null>(null)
function isValidUploadLimit(n: number, ceiling?: number): boolean {
  if (!Number.isInteger(n) || n < 1) return false
  return ceiling !== undefined && n <= ceiling
}

// One draft with one Save (doc/settings-ux.md). "Back to the default" only sets the draft to the ceiling.
// An older server knows no metadata switch: it is then neither shown nor sent.
const hasStripSwitch = computed(() => settings.value?.strip_image_metadata !== undefined)
const form = useSettingsDraft<{ limit: string | number, strip: boolean }>({
  source: () => ({ limit: settings.value ? String(settings.value.max_upload_mb) : '', strip: settings.value?.strip_image_metadata ?? true }),
  normalisers: { limit: value => (String(value).trim() === '' ? '' : normalise.count(value)) },
  validate: draft => (isValidUploadLimit(Number(draft.limit), settings.value?.ceiling_mb) ? {} : { limit: t('admin.uploadLimit.invalid', { ceiling: settings.value?.ceiling_mb ?? 0 }) }),
  blocked: () => !settings.value,
  save: async ({ draft }) => {
    // The ceiling is the default: it is stored as "no override".
    const value = Number(draft.limit) === settings.value?.ceiling_mb ? null : Number(draft.limit)
    const json = { max_upload_mb: value, ...(hasStripSwitch.value ? { strip_image_metadata: draft.strip } : {}) }
    settings.value = await api('/api/admin/upload-limit', { method: 'PUT', json, decode: decodePermUploadSettings })
  },
  saved: () => { toasts.success(t('admin.uploadLimit.saved')) }
})
const { draft, errors } = form

async function load() {
  try {
    settings.value = await api('/api/admin/upload-limit', { decode: decodePermUploadSettings })
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  }
}

onMounted(load)
</script>

<template>
  <div v-if="settings" class="rounded-lg border border-mnema-hairline bg-mnema-surface" data-testid="upload-limit">
    <form @submit.prevent="form.save" @keydown="form.onKeydown">
      <div class="space-y-2 p-4">
        <div class="text-sm font-semibold text-mnema-text">{{ $t('admin.uploadLimit.title') }}</div>
        <p class="text-xs text-mnema-tertiary">{{ $t('admin.uploadLimit.hint', { ceiling: settings.ceiling_mb }) }}</p>
        <div class="flex flex-wrap items-center gap-3">
          <label class="flex items-center gap-2 text-sm text-mnema-text">
            <span>{{ $t('admin.uploadLimit.label') }}</span>
            <input v-model="draft.limit" type="number" inputmode="numeric" min="1" :max="settings.ceiling_mb" step="1" :aria-label="$t('admin.uploadLimit.label')" :aria-invalid="errors.limit ? 'true' : undefined" class="w-24 rounded-md border border-mnema-border-field bg-mnema-surface px-2 py-1 text-sm tabular-nums text-mnema-text outline-hidden focus:border-mnema-accent" />
            <span class="text-mnema-tertiary">MB</span>
          </label>
          <button v-if="Number(draft.limit) !== settings.ceiling_mb" type="button" class="text-sm text-mnema-tertiary transition hover:text-mnema-text" @click="draft.limit = String(settings.ceiling_mb)">{{ $t('admin.uploadLimit.reset') }}</button>
        </div>
        <p v-if="String(draft.limit) !== '' && errors.limit" class="text-xs text-mnema-danger" role="alert">{{ errors.limit }}</p>
        <div v-if="hasStripSwitch" class="space-y-1 pt-2" data-testid="strip-metadata-setting">
          <label class="flex items-center gap-2 text-sm text-mnema-text">
            <input v-model="draft.strip" type="checkbox" data-testid="strip-metadata" />
            <span>{{ $t('admin.uploadLimit.stripMetadata') }}</span>
          </label>
          <p class="text-xs text-mnema-tertiary">{{ $t('admin.uploadLimit.stripMetadataHint') }}</p>
        </div>
      </div>
      <UnsavedChangesBar :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
    </form>
  </div>
</template>
