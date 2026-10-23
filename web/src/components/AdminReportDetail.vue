<script setup lang="ts">
// One report: the message as it was when it was filed with the messages around
// it, the reporter's note, and the moderator's way to close it (resolved:
// acted on, dismissed: no action) or, once closed, to delete it. The
// reporter is not told.
import { computed, onMounted, ref } from 'vue'
import { ArrowLeft, Loader2, Paperclip } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeChatReportDetail, type ChatReportDetail } from '../types/rest'
import { confirm } from '../lib/confirm'
import { REPORT_NOTE_MAX } from '../lib/reportDialog'
import { useToastStore } from '../stores/toast'
import { locale, t } from '../i18n'

const props = defineProps({
  id: { type: String, required: true }
})
const emit = defineEmits<{ back: []; changed: [] }>()

const toasts = useToastStore()
const report = ref<ChatReportDetail | null>(null)
const note = ref('')
const busy = ref(false)
const failed = ref(false)

const when = (iso: string) => new Date(iso).toLocaleString(locale.value, { dateStyle: 'medium', timeStyle: 'medium' })
const isOpen = computed(() => report.value?.status === 'open')
// The messages around the reported one, with the reported one in its place (the server keeps it apart).
const copy = computed(() => {
  const snapshot = report.value?.snapshot
  if (!snapshot) return []
  const stamp = (m: { created_at: string }) => Date.parse(m.created_at)
  return [...snapshot.context, snapshot.message].sort((a, b) => stamp(a) - stamp(b))
})

async function load() {
  try {
    report.value = await api(`/api/admin/reports/${props.id}`, { decode: decodeChatReportDetail })
  } catch (error) {
    failed.value = true
    toasts.error(caughtErrorMessage(error, t('reports.admin.loadFailed')))
  }
}

async function close(status: 'resolved' | 'dismissed') {
  if (busy.value) return
  busy.value = true
  try {
    await api(`/api/admin/reports/${props.id}/resolve`, { method: 'POST', json: { status, note: note.value.trim() } })
    toasts.success(t(status === 'resolved' ? 'reports.admin.resolvedToast' : 'reports.admin.dismissedToast'))
    emit('changed')
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('reports.admin.closeFailed')))
  } finally {
    busy.value = false
  }
}

async function remove() {
  const ok = await confirm({
    title: t('reports.admin.deleteTitle'),
    body: t('reports.admin.deleteBody'),
    confirmLabel: t('reports.admin.delete'),
    cancelLabel: t('common.cancel'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/reports/${props.id}`, { method: 'DELETE' })
    emit('changed')
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('reports.admin.deleteFailed')))
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4" data-testid="report-detail">
    <button type="button" data-testid="report-back" class="flex items-center gap-1.5 text-sm text-mnema-accent hover:underline" @click="emit('back')">
      <ArrowLeft class="h-4 w-4" />{{ $t('reports.admin.back') }}
    </button>

    <p v-if="failed" role="alert" class="text-sm text-mnema-danger">{{ $t('reports.admin.loadFailed') }}</p>
    <Loader2 v-else-if="!report" class="h-5 w-5 animate-spin text-mnema-accent" />

    <template v-else>
      <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
        <dt class="text-mnema-tertiary">{{ $t('reports.admin.reason') }}</dt>
        <dd class="text-mnema-text">{{ $t(`reports.category.${report.category}`) }}</dd>
        <dt class="text-mnema-tertiary">{{ $t('reports.admin.reporter') }}</dt>
        <dd class="text-mnema-text">{{ report.reporter?.username }}</dd>
        <dt class="text-mnema-tertiary">{{ $t('reports.admin.reported') }}</dt>
        <dd class="text-mnema-text">{{ report.reported?.username }}</dd>
        <dt class="text-mnema-tertiary">{{ $t('reports.admin.where') }}</dt>
        <dd class="text-mnema-text">{{ report.kind === 'dm' ? $t('reports.admin.dm') : `#${report.channel_name ?? ''}` }}</dd>
        <template v-if="report.note">
          <dt class="text-mnema-tertiary">{{ $t('reports.admin.note') }}</dt>
          <dd class="wrap-break-word text-mnema-text" data-testid="report-note-text">{{ report.note }}</dd>
        </template>
      </dl>

      <section :aria-label="$t('reports.admin.copy')">
        <h4 class="mb-1 text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('reports.admin.copy') }}</h4>
        <p v-if="report.kind === 'dm'" class="mb-2 text-xs text-mnema-tertiary">{{ $t('reports.admin.dmNotice') }}</p>
        <ol class="max-h-96 space-y-1 overflow-y-auto rounded-lg border border-mnema-hairline bg-mnema-surface p-2" data-testid="report-context">
          <li
            v-for="msg in copy"
            :key="msg.id"
            :data-reported="msg.id === report.snapshot.message.id ? 'true' : undefined"
            :class="['rounded-md px-3 py-1.5 text-sm', msg.id === report.snapshot.message.id ? 'border border-mnema-danger/40 bg-mnema-danger/10' : '']"
          >
            <p class="flex items-baseline gap-2 text-xs text-mnema-tertiary">
              <span class="font-semibold text-mnema-text">{{ msg.display_name || msg.username }}</span>{{ when(msg.created_at) }}
            </p>
            <p class="whitespace-pre-wrap wrap-break-word text-mnema-body-ink">{{ msg.content }}</p>
            <p v-for="file in msg.attachments" :key="file.filename" class="flex items-center gap-1 text-xs text-mnema-accent"><Paperclip class="h-3 w-3" />{{ file.filename }}</p>
          </li>
        </ol>
      </section>

      <form v-if="isOpen" class="space-y-2" @submit.prevent>
        <label for="report-resolution" class="block text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('reports.admin.resolutionNote') }}</label>
        <textarea id="report-resolution" v-model="note" rows="2" :maxlength="REPORT_NOTE_MAX" data-testid="report-resolution" class="w-full rounded-lg border border-mnema-border bg-mnema-canvas px-3 py-2 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"></textarea>
        <div class="flex gap-2">
          <button type="button" :disabled="busy" data-testid="report-resolve" class="rounded-md bg-mnema-accent px-3 py-1.5 text-sm font-semibold text-mnema-accent-ink transition hover:bg-mnema-accent-hover disabled:opacity-40" @click="close('resolved')">{{ $t('reports.admin.resolve') }}</button>
          <button type="button" :disabled="busy" data-testid="report-dismiss" class="rounded-md border border-mnema-border px-3 py-1.5 text-sm font-semibold text-mnema-text transition hover:bg-mnema-hover disabled:opacity-40" @click="close('dismissed')">{{ $t('reports.admin.dismiss') }}</button>
        </div>
      </form>
      <div v-else class="space-y-2">
        <p class="text-sm text-mnema-muted" data-testid="report-closed">
          {{ $t(`reports.admin.closedAs.${report.status}`, { name: report.resolved_by?.username ?? '', date: report.resolved_at ? when(report.resolved_at) : '' }) }}
        </p>
        <p v-if="report.resolution_note" class="wrap-break-word text-sm text-mnema-text">{{ report.resolution_note }}</p>
        <button type="button" data-testid="report-delete" class="rounded-md border border-mnema-danger/50 px-3 py-1.5 text-sm text-mnema-danger transition hover:bg-mnema-danger/10" @click="remove">{{ $t('reports.admin.delete') }}</button>
      </div>
    </template>
  </div>
</template>
