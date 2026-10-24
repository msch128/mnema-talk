<script setup lang="ts">
// Admin dashboard: the moderation queue of reported messages (channel messages
// and direct messages alike). The list holds no message text; opening a report
// shows the copy the reporter's report kept. Needs Manage reports.
import { onMounted, ref, watch } from 'vue'
import { Flag, Loader2, MessageCircle } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeChatReportsPage, type ChatReport } from '../types/rest'
import { useToastStore } from '../stores/toast'
import { locale, t } from '../i18n'
import AdminReportDetail from './AdminReportDetail.vue'

const PAGE = 25
type StatusFilter = 'open' | 'resolved' | 'dismissed' | 'all'
const FILTERS: readonly StatusFilter[] = ['open', 'resolved', 'dismissed', 'all']

const toasts = useToastStore()
const status = ref<StatusFilter>('open')
const reports = ref<ChatReport[]>([])
const hasMore = ref(false)
const loading = ref(false)
const selected = ref<string | null>(null)
let seq = 0

async function load(more = false) {
  const mine = ++seq
  loading.value = true
  const params = new URLSearchParams({ status: status.value, limit: String(PAGE) })
  const last = reports.value[reports.value.length - 1]
  if (more && last) params.set('before', last.id)
  try {
    const page = await api(`/api/admin/reports?${params.toString()}`, { decode: decodeChatReportsPage })
    if (mine !== seq) return
    reports.value = more ? [...reports.value, ...page.reports] : page.reports
    hasMore.value = page.has_more
  } catch (error) {
    if (mine === seq) toasts.error(caughtErrorMessage(error, t('reports.admin.loadFailed')))
  } finally {
    if (mine === seq) loading.value = false
  }
}

watch(status, () => {
  selected.value = null
  void load()
})

function onChanged() {
  selected.value = null
  void load()
}

const when = (iso: string) => new Date(iso).toLocaleString(locale.value, { dateStyle: 'medium', timeStyle: 'short' })
const where = (report: ChatReport) => (report.kind === 'dm' ? t('reports.admin.dm') : report.kind === 'alert' ? t('reports.admin.alertWhere') : `#${report.channel_name ?? ''}`)
const isAutomod = (report: ChatReport) => report.source === 'automod'

onMounted(() => load())
</script>

<template>
  <section class="space-y-4" data-testid="admin-reports">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h3 class="flex items-center gap-2 font-mono text-sm font-semibold uppercase tracking-wider text-mnema-text">
        <Flag class="h-4 w-4 text-mnema-accent" />
        <span>{{ $t('reports.admin.title') }}</span>
      </h3>
      <div role="group" :aria-label="$t('reports.admin.filter')" class="flex gap-1" data-testid="report-filter">
        <button
          v-for="option in FILTERS"
          :key="option"
          type="button"
          :aria-pressed="status === option ? 'true' : 'false'"
          :data-filter="option"
          :class="['rounded-md border px-2.5 py-1 text-xs font-semibold transition', status === option ? 'border-mnema-accent/40 bg-mnema-accent/15 text-mnema-accent' : 'border-transparent text-mnema-muted hover:text-mnema-text']"
          @click="status = option"
        >{{ $t(`reports.admin.status.${option}`) }}</button>
      </div>
    </div>
    <p class="text-xs text-mnema-tertiary">{{ $t('reports.admin.hint') }}</p>

    <AdminReportDetail v-if="selected" :id="selected" @back="selected = null" @changed="onChanged" />

    <div v-else class="divide-y divide-mnema-hairline overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
      <div v-if="!reports.length && !loading" class="p-4 text-sm text-mnema-tertiary" data-testid="reports-empty">{{ $t('reports.admin.empty') }}</div>
      <button
        v-for="report in reports"
        :key="report.id"
        type="button"
        :data-report="report.id"
        class="flex w-full items-start justify-between gap-3 px-4 py-2.5 text-left text-sm transition hover:bg-mnema-hover focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-mnema-accent"
        @click="selected = report.id"
      >
        <span class="min-w-0 space-y-0.5">
          <span class="flex min-w-0 flex-wrap items-center gap-2">
            <span class="rounded-sm bg-mnema-band px-1.5 py-0.5 text-xs font-semibold text-mnema-text">{{ $t(`reports.category.${report.category}`) }}</span>
            <span class="flex items-center gap-1 text-xs text-mnema-muted">
              <MessageCircle v-if="report.kind === 'dm'" class="h-3 w-3" aria-hidden="true" />{{ where(report) }}
            </span>
            <span class="text-xs text-mnema-tertiary">{{ when(report.created_at) }}</span>
          </span>
          <span v-if="isAutomod(report)" class="block truncate text-xs text-mnema-muted" data-testid="report-automod">
            <span class="mr-1.5 rounded-sm bg-mnema-accent/15 px-1.5 py-0.5 font-semibold text-mnema-accent">{{ $t('reports.admin.automod') }}</span>{{ report.kind === 'alert'
              ? $t('reports.admin.raidAlert', { rule: report.automod_rule ?? '' })
              : $t(`reports.admin.automodHit.${report.automod_outcome ?? 'flagged'}`, { rule: report.automod_rule ?? '', reported: report.reported?.username ?? '' }) }}<template v-if="(report.hit_count ?? 1) > 1"> ×{{ report.hit_count }}</template>
          </span>
          <span v-else class="block truncate text-xs text-mnema-muted">
            {{ $t('reports.admin.by', { reporter: report.reporter?.username ?? '', reported: report.reported?.username ?? '' }) }}
          </span>
          <span v-if="report.note" class="block truncate text-xs text-mnema-tertiary">{{ report.note }}</span>
        </span>
        <span :class="['shrink-0 text-xs font-semibold', report.status === 'open' ? 'text-mnema-amber' : 'text-mnema-tertiary']">{{ $t(`reports.admin.status.${report.status}`) }}</span>
      </button>
    </div>

    <div v-if="!selected" class="flex justify-center">
      <Loader2 v-if="loading" class="h-5 w-5 animate-spin text-mnema-accent" />
      <button v-else-if="hasMore" type="button" data-testid="reports-more" class="btn btn-secondary" @click="load(true)">{{ $t('common.loadMore') }}</button>
    </div>
  </section>
</template>
