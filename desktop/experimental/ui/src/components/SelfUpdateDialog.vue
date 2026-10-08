<script setup lang="ts">
// Confirms a self-update: current → new version, a link to the changes, a
// warning that everyone is disconnected, and the admin's password, which the
// server checks again before it asks the updater sidecar.
import { ref } from 'vue'
import { AlertTriangle } from '@lucide/vue'
import BaseDialog from './BaseDialog.vue'
import { api, isApiError, caughtErrorMessage } from '../lib/api'
import { decodeServerSelfUpdateStarted, type ServerSelfUpdateStarted } from '../types/rest'
import { t } from '../i18n'

const props = defineProps({
  currentVersion: { type: String, required: true },
  targetVersion: { type: String, required: true },
  releaseUrl: { type: String, default: '' }
})
const emit = defineEmits<{ close: []; started: [result: ServerSelfUpdateStarted] }>()

const password = ref('')
const busy = ref(false)
const error = ref('')

async function submit() {
  if (!password.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const res = await api('/api/admin/system/update', {
      method: 'POST',
      json: { password: password.value, target_version: props.targetVersion },
      decode: decodeServerSelfUpdateStarted
    })
    password.value = ''
    emit('started', res)
  } catch (e) {
    password.value = ''
    error.value = isApiError(e) && e.code === 'RATE_LIMITED' ? t('admin.system.selfUpdateRateLimited') : caughtErrorMessage(e, t('admin.unknownError'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseDialog :title="$t('admin.system.selfUpdateTitle')" role="alertdialog" initial-focus="#self-update-password" @close="emit('close')">
    <form class="p-5 space-y-4 overflow-y-auto" data-testid="self-update-dialog" @submit.prevent="submit">
      <p class="text-base font-mono text-mnema-text" data-testid="self-update-versions">{{ currentVersion }} → {{ targetVersion }}</p>
      <a
        v-if="releaseUrl"
        :href="releaseUrl"
        target="_blank"
        rel="noopener noreferrer"
        class="text-sm text-mnema-accent hover:underline"
      >{{ $t('admin.system.releaseNotesLink', { version: targetVersion }) }}</a>
      <div class="flex gap-2 rounded-lg border border-mnema-warning/35 bg-mnema-warning/10 p-3 text-sm text-mnema-text">
        <AlertTriangle class="h-4 w-4 flex-shrink-0 text-mnema-warning mt-0.5" />
        <span>{{ $t('admin.system.selfUpdateWarning') }}</span>
      </div>
      <div v-if="error" role="alert" class="p-2.5 rounded-lg bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm" data-testid="self-update-error">
        {{ error }}
      </div>
      <div class="space-y-1.5">
        <label for="self-update-password" class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">
          {{ $t('admin.system.selfUpdatePassword') }}
        </label>
        <input
          id="self-update-password"
          v-model="password"
          type="password"
          autocomplete="current-password"
          required
          class="w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text outline-none focus:border-mnema-accent"
        />
      </div>
      <div class="flex justify-end gap-2 pt-1">
        <button type="button" class="px-3 py-1.5 rounded-md text-sm text-mnema-muted hover:text-mnema-text" @click="emit('close')">
          {{ $t('common.cancel') }}
        </button>
        <button
          type="submit"
          data-testid="self-update-confirm"
          :disabled="!password || busy"
          class="px-4 py-1.5 rounded-md bg-mnema-accent hover:bg-mnema-accent-hover text-mnema-accent-ink text-sm font-semibold transition disabled:opacity-40"
        >
          {{ busy ? $t('admin.system.selfUpdateStarting') : $t('admin.system.selfUpdateButton') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>
