<script setup lang="ts">
// Owner only (System tab): the global upload limit. MAX_UPLOAD_SIZE_MB stays
// the operator's ceiling; this can only lower it. Roles may override it.
import { ref, computed, onMounted } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodePermUploadSettings } from '../types/rest'
import type { PermUploadSettings } from '../types/rest'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

const toasts = useToastStore()
const settings = ref<PermUploadSettings | null>(null)
const draft = ref<string | number>('')
const busy = ref(false)

const parsed = computed(() => {
  const n = Number(draft.value)
  return Number.isInteger(n) && n >= 1 && settings.value && n <= settings.value.ceiling_mb ? n : null
})
const dirty = computed(() => !!settings.value && parsed.value !== null && parsed.value !== settings.value.max_upload_mb)
const atCeiling = computed(() => !!settings.value && settings.value.max_upload_mb === settings.value.ceiling_mb)

function apply(next: PermUploadSettings) {
  settings.value = next
  draft.value = String(next.max_upload_mb)
}

async function load() {
  try {
    apply(await api('/api/admin/upload-limit', { decode: decodePermUploadSettings }))
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  }
}

async function save(value: number | null) {
  busy.value = true
  try {
    apply(await api('/api/admin/upload-limit', { method: 'PUT', json: { max_upload_mb: value }, decode: decodePermUploadSettings }))
    toasts.success(t('admin.uploadLimit.saved'))
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-if="settings" class="space-y-2 rounded-lg border border-mnema-hairline bg-mnema-surface p-4" data-testid="upload-limit">
    <div class="text-sm font-semibold text-mnema-text">{{ $t('admin.uploadLimit.title') }}</div>
    <p class="text-xs text-mnema-tertiary">{{ $t('admin.uploadLimit.hint', { ceiling: settings.ceiling_mb }) }}</p>
    <form class="flex flex-wrap items-center gap-2" @submit.prevent="parsed !== null && save(parsed)">
      <label class="flex items-center gap-2 text-sm text-mnema-text">
        <span>{{ $t('admin.uploadLimit.label') }}</span>
        <input v-model="draft" type="number" inputmode="numeric" min="1" :max="settings.ceiling_mb" step="1" :aria-label="$t('admin.uploadLimit.label')" class="w-24 rounded-md border border-mnema-border-field bg-mnema-surface px-2 py-1 text-sm tabular-nums text-mnema-text outline-none focus:border-mnema-accent" />
        <span class="text-mnema-tertiary">MB</span>
      </label>
      <button type="submit" :disabled="!dirty || busy" class="rounded-md bg-mnema-accent px-3 py-1 text-sm font-semibold text-mnema-accent-ink shadow-sm transition hover:bg-mnema-accent-hover disabled:opacity-40">{{ $t('admin.uploadLimit.save') }}</button>
      <button v-if="!atCeiling" type="button" :disabled="busy" class="text-sm text-mnema-tertiary transition hover:text-mnema-text" @click="save(null)">{{ $t('admin.uploadLimit.reset') }}</button>
    </form>
    <p v-if="draft !== '' && parsed === null" class="text-xs text-mnema-danger" role="alert">{{ $t('admin.uploadLimit.invalid', { ceiling: settings.ceiling_mb }) }}</p>
  </div>
</template>
