<script setup lang="ts">
// Host for openReport() from lib/reportDialog: tell the moderators about a
// message. The report keeps a copy of the message and the ones around it, so
// it still stands if the message is deleted; the author is not told.
import { ref, watch } from 'vue'
import { Loader2 } from '@lucide/vue'
import { api, isApiError } from '../lib/api'
import { closeReport, pendingReport, REPORT_CATEGORIES, REPORT_NOTE_MAX, type ReportCategory } from '../lib/reportDialog'
import { decodeChatReportFiled } from '../types/rest'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const toasts = useToastStore()
const category = ref<ReportCategory>('spam')
const note = ref('')
const busy = ref(false)
const error = ref('')

// Every report starts from a clean form.
watch(pendingReport, () => {
  category.value = 'spam'
  note.value = ''
  error.value = ''
})

function failure(err: unknown): string {
  if (!isApiError(err)) return t('reports.failed')
  if (err.status === 404) return t('reports.gone')
  if (err.status === 409) return t('reports.tooMany')
  if (err.status === 429) return t('reports.slowDown')
  return t('reports.failed')
}

async function send() {
  const request = pendingReport.value
  if (!request || busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api(`/api/messages/${request.messageId}/report`, {
      method: 'POST',
      json: { category: category.value, note: note.value.trim() },
      decode: decodeChatReportFiled
    })
    toasts.success(t('reports.sent'))
    closeReport()
  } catch (err) {
    error.value = failure(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseDialog v-if="pendingReport" :title="$t('reports.title')" panel-class="max-w-md" initial-focus="[data-report-first]" @close="closeReport">
    <form class="flex min-h-0 flex-col gap-4 overflow-y-auto p-5" data-testid="report-dialog" @submit.prevent="send">
      <p class="text-sm text-mnema-muted">{{ $t('reports.intro', { name: pendingReport.author }) }}</p>

      <fieldset class="stack-1">
        <legend class="mb-1 text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('reports.reason') }}</legend>
        <label
          v-for="(option, i) in REPORT_CATEGORIES"
          :key="option"
          :class="['flex cursor-pointer items-start gap-2 rounded-md border px-3 py-2 transition max-md:min-h-11', category === option ? 'border-mnema-accent/50 bg-mnema-accent/10' : 'border-mnema-border hover:bg-mnema-hover']"
        >
          <input v-model="category" type="radio" name="report-category" :value="option" :data-report-first="i === 0 ? '' : undefined" :data-report-category="option" class="mt-1 accent-mnema-accent" />
          <span>
            <span class="block text-sm font-medium text-mnema-text">{{ $t(`reports.category.${option}`) }}</span>
            <span class="block text-xs text-mnema-tertiary">{{ $t(`reports.categoryHint.${option}`) }}</span>
          </span>
        </label>
      </fieldset>

      <div class="space-y-1">
        <label for="report-note" class="block text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('reports.note') }}</label>
        <textarea id="report-note" v-model="note" rows="3" :maxlength="REPORT_NOTE_MAX" :placeholder="$t('reports.notePlaceholder')" data-testid="report-note" class="w-full rounded-lg border border-mnema-border bg-mnema-canvas px-3 py-2 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"></textarea>
        <p class="text-right text-xs text-mnema-tertiary">{{ note.length }} / {{ REPORT_NOTE_MAX }}</p>
      </div>

      <p class="text-xs text-mnema-tertiary">{{ $t('reports.privacy') }}</p>
      <div v-if="error" role="alert" data-testid="report-error" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-2 text-sm text-mnema-danger">{{ error }}</div>

      <div class="flex justify-end gap-2 border-t border-mnema-hairline pt-3">
        <button type="button" class="h-8 rounded-md border border-mnema-border px-3 text-sm font-semibold text-mnema-text transition hover:bg-mnema-hover max-md:h-11" @click="closeReport">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="busy" data-testid="report-submit" class="flex h-8 items-center gap-1.5 rounded-md bg-mnema-accent px-3 text-sm font-semibold text-mnema-accent-ink transition hover:bg-mnema-accent-hover disabled:opacity-40 max-md:h-11">
          <Loader2 v-if="busy" class="h-4 w-4 animate-spin" />{{ $t('reports.send') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>
