<script setup lang="ts">
// Capacity panel of Admin > Status: live CPU, memory and media rates, and per usage scenario how many people one
// Talk can hold on what is free (GET /api/admin/status/capacity), with the resource that runs out first. The
// estimate is labelled as one and links the benchmark it comes from.
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { Gauge } from '@lucide/vue'
import { api } from '../lib/api'
import { formatFileSize } from '../lib/fileSize'
import { SOURCE_URL } from '../lib/thirdParty'
import { decodeServerCapacityStatus, type ServerCapacityScenario, type ServerCapacityStatus } from '../types/rest'
import { locale, t } from '../i18n'

const REFRESH_MS = 10_000
const BENCHMARK_URL = `${SOURCE_URL}/blob/main/doc/benchmark.md#sizing-guide`

const cap = ref<ServerCapacityStatus | null>(null)
const failed = ref(false)
let timer: ReturnType<typeof setInterval> | undefined
let inFlight = false

async function load() {
  if (inFlight) return
  inFlight = true
  try {
    cap.value = await api('/api/admin/status/capacity', { decode: decodeServerCapacityStatus })
    failed.value = false
  } catch {
    // The last figures stay; before the first answer a hint says so.
    failed.value = true
  } finally {
    inFlight = false
  }
}

// A hidden tab does not poll; coming back loads at once.
function tick() {
  if (document.visibilityState !== 'hidden') void load()
}

onMounted(() => {
  void load()
  timer = setInterval(tick, REFRESH_MS)
  document.addEventListener('visibilitychange', tick)
})
onUnmounted(() => {
  clearInterval(timer)
  document.removeEventListener('visibilitychange', tick)
})

const num = (v: number, digits = 1) => new Intl.NumberFormat(locale.value, { maximumFractionDigits: digits }).format(v)
const size = (bytes: number) => formatFileSize(bytes, locale.value)

const p = 'admin.operatorStatus.capacity.'

// One row: the scenario, how many fit, what limits it and how sure the figure is.
function scenarioRow(s: ServerCapacityScenario) {
  let basis = t(`${p}derived`)
  if (s.basis === 'measured') basis = s.extrapolated ? t(`${p}beyondMeasured`, { count: num(s.measured_up_to, 0) }) : t(`${p}measuredUpTo`, { count: num(s.measured_up_to, 0) })
  const cost = [t(`${p}limitedBy`, { limit: t(`${p}limit.${s.limit}`) })]
  if (s.upload_mbps > 0) cost.push(t(`${p}upload`, { mbps: num(s.upload_mbps, 0) }))
  return {
    id: s.id,
    label: t(`${p}scenario.${s.id}.label`),
    detail: t(`${p}scenario.${s.id}.detail`),
    basis,
    people: t(`${p}people`, { count: num(s.members, 0) }),
    cost: cost.join(' · ')
  }
}

// The note's sentence around the benchmark link, split where the link goes.
function noteParts(cores: number, basis: string) {
  const marker = '\u0001'
  const [before = '', after = ''] = t(`${p}note`, { cores: num(cores, 2), basis, link: marker }).split(marker)
  return { before, after }
}

// Everything the template shows, worked out once from the last answer.
const view = computed(() => {
  const c = cap.value
  if (!c || c.collecting) return null
  const e = c.estimate
  const basis = e.basis === 'observed' ? t(`${p}basisObserved`, { scale: num(e.scale, 2) }) : t(`${p}basisTable`)
  return {
    status: e.status,
    cpu: t(`${p}cpuValue`, { used: num(c.cpu_used_cores, 2), total: num(c.cpu_cores, 2) }),
    memory: c.memory_limit_bytes > 0
      ? t(`${p}memoryValue`, { used: size(c.rss_bytes), limit: size(c.memory_limit_bytes) })
      : t(`${p}memoryNoLimit`, { used: size(c.rss_bytes) }),
    media: t(`${p}mediaValue`, { mbps: num(c.egress_mbps), pps: num(c.egress_pps, 0) }),
    calls: t(`${p}callsValue`, { members: t(`${p}members`, { count: c.peers }), shares: t(`${p}shares`, { count: c.screen_shares }) }),
    verdict: e.status === 'ok' ? t(`${p}estimateHeading`) : t(`${p}estimate_${e.status}`),
    scenarios: e.status === 'ok' ? (e.scenarios ?? []).map(scenarioRow) : [],
    uploadUnknown: e.status === 'ok' && e.upload_limit_mbps === 0,
    note: noteParts(e.plan_cores, basis)
  }
})
</script>

<template>
  <div v-if="cap?.collecting || (failed && !cap)" class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline" data-testid="status-capacity-pending">
    <p class="text-xs text-mnema-muted">{{ cap?.collecting ? $t('admin.operatorStatus.capacity.collecting') : $t('admin.operatorStatus.capacity.failed') }}</p>
  </div>
  <div v-else-if="view" class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3" data-testid="status-capacity">
    <div class="flex items-center gap-2">
      <Gauge class="w-4 h-4 text-mnema-accent shrink-0" aria-hidden="true" />
      <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.operatorStatus.capacity.heading') }}</span>
    </div>
    <dl class="grid grid-cols-1 min-[420px]:grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
      <div>
        <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.capacity.cpu') }}</dt>
        <dd class="font-mono text-mnema-text" data-testid="capacity-cpu">{{ view.cpu }}</dd>
      </div>
      <div>
        <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.capacity.memory') }}</dt>
        <dd class="font-mono text-mnema-text" data-testid="capacity-memory">{{ view.memory }}</dd>
      </div>
      <div>
        <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.capacity.media') }}</dt>
        <dd class="font-mono text-mnema-text" data-testid="capacity-media">{{ view.media }}</dd>
      </div>
      <div>
        <dt class="text-mnema-tertiary text-xs">{{ $t('admin.operatorStatus.capacity.calls') }}</dt>
        <dd class="font-mono text-mnema-text" data-testid="capacity-calls">{{ view.calls }}</dd>
      </div>
    </dl>
    <div class="pt-3 border-t border-mnema-hairline space-y-2">
      <p class="text-sm font-medium text-mnema-text" :data-status="view.status" data-testid="capacity-estimate">{{ view.verdict }}</p>
      <ul v-if="view.scenarios.length" class="divide-y divide-mnema-hairline" data-testid="capacity-scenarios">
        <!-- Phone: the figure follows the label on the left; from sm up it sits in a right-aligned column. -->
        <li v-for="row in view.scenarios" :key="row.id" class="py-2.5 flex flex-col gap-1 sm:flex-row sm:items-baseline sm:justify-between sm:gap-x-4" :data-testid="`capacity-scenario-${row.id}`">
          <div class="min-w-0">
            <p class="text-sm text-mnema-text">{{ row.label }} <span class="text-mnema-muted">– {{ row.detail }}</span></p>
            <p class="text-xs text-mnema-tertiary" data-testid="capacity-scenario-basis">{{ row.basis }}</p>
          </div>
          <div class="flex flex-wrap items-baseline gap-x-2 sm:block sm:shrink-0 sm:text-right">
            <p class="font-mono text-sm text-mnema-text" data-testid="capacity-scenario-people">{{ row.people }}</p>
            <p class="text-xs text-mnema-muted" data-testid="capacity-scenario-cost">{{ row.cost }}</p>
          </div>
        </li>
      </ul>
      <p v-if="view.uploadUnknown" class="text-xs text-mnema-muted" data-testid="capacity-upload-unknown">{{ $t('admin.operatorStatus.capacity.uploadUnknown') }}</p>
      <p class="text-xs text-mnema-muted" data-testid="capacity-assumption">{{ view.note.before }}<a :href="BENCHMARK_URL" target="_blank" rel="noopener noreferrer" class="text-mnema-accent hover:underline">{{ $t('admin.operatorStatus.capacity.benchmarkLink') }}</a>{{ view.note.after }}</p>
    </div>
  </div>
</template>
