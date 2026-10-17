<script setup lang="ts">
// Live operator view of the System tab: the same numbers Prometheus reads
// (GET /api/admin/system/ops), with a one-hour history as sparklines.
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { api } from '../lib/api'
import { decodeOpsStatus } from '../types/domain'
import type { OpsStatus, OpsSample } from '../types/domain'
import { t, locale } from '../i18n'
import Sparkline from './Sparkline.vue'

const REFRESH_MS = 10_000

const EMPTY: OpsStatus = {
  interval_seconds: 10,
  history: [],
  totals: {
    requests: 0, errors_4xx: 0, errors_5xx: 0, rate_limited: 0, ws_connections: 0, ws_dropped: 0,
    mls_handshakes_relayed: 0, mls_handshakes_rejected: 0, ice_connected: 0, ice_failed: 0, ice_disconnected: 0,
    nack_requests: 0, keyframe_requests: 0, layer_switches: 0, egress_bytes: 0, ingress_bytes: 0,
    storage_errors: 0, storage_operations: 0
  },
  db_pool: { total: 0, idle: 0, acquired: 0, max: 0, waited: 0 },
  process: { cpu_ms: 0, rss_bytes: 0, heap_bytes: 0, goroutines: 0, gc_cycles: 0, gc_pause_ms: 0 }
}

const ops = ref<OpsStatus>(EMPTY)
// Nothing is drawn before the first answer arrives.
const loaded = ref(false)
const failed = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

async function refresh() {
  // A hidden tab does not need fresh numbers.
  if (document.hidden) return
  try {
    ops.value = await api('/api/admin/system/ops', { decode: decodeOpsStatus })
    loaded.value = true
    failed.value = false
  } catch {
    failed.value = true
  }
}

onMounted(() => {
  void refresh()
  timer = setInterval(() => void refresh(), REFRESH_MS)
})
onBeforeUnmount(() => {
  clearInterval(timer)
})
defineExpose({ refresh })

const nf = computed(() => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }))
const int = computed(() => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }))

function bytes(n: number) {
  if (!n) return '0 B'
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(sizes.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  return `${nf.value.format(n / Math.pow(1024, i))} ${sizes[i]}`
}

function rate(bps: number) {
  const units = ['bit/s', 'kbit/s', 'Mbit/s', 'Gbit/s']
  const i = bps >= 1e9 ? 3 : bps >= 1e6 ? 2 : bps >= 1e3 ? 1 : 0
  return `${nf.value.format(bps / Math.pow(1000, i))} ${units[i]}`
}

interface Tile {
  key: string
  label: string
  series: (s: OpsSample) => number
  format: (n: number) => string
  /** Highlights the tile when the latest value is above zero. */
  warn?: boolean
}

const tiles: Tile[] = [
  { key: 'cpu', label: 'cpu', series: s => s.cpu_millicores, format: n => `${nf.value.format(n / 10)} %` },
  // Resident memory where the platform reports it, else the Go heap.
  { key: 'memory', label: 'memory', series: s => s.rss_bytes || s.heap_bytes, format: bytes },
  { key: 'goroutines', label: 'goroutines', series: s => s.goroutines, format: n => int.value.format(n) },
  { key: 'online', label: 'online', series: s => s.online_users, format: n => int.value.format(n) },
  { key: 'talks', label: 'talks', series: s => s.peers, format: n => int.value.format(n) },
  { key: 'screens', label: 'screens', series: s => s.screen_shares, format: n => int.value.format(n) },
  { key: 'egress', label: 'egress', series: s => s.egress_bps, format: rate },
  { key: 'ingress', label: 'ingress', series: s => s.ingress_bps, format: rate },
  { key: 'requests', label: 'requests', series: s => s.requests, format: n => nf.value.format(n / interval.value) },
  { key: 'errors', label: 'errors', series: s => s.errors_5xx, format: n => int.value.format(n), warn: true },
  { key: 'iceFailures', label: 'iceFailures', series: s => s.ice_failures, format: n => int.value.format(n), warn: true },
  { key: 'dbPool', label: 'dbPool', series: s => s.db_connections_acquired, format: n => int.value.format(n) },
]

const interval = computed(() => Math.max(1, ops.value.interval_seconds))
const history = computed(() => ops.value.history)

const cards = computed(() => tiles.map((tile) => {
  const values = history.value.map(tile.series)
  const now = values.length ? values[values.length - 1]! : 0
  const peak = values.length ? Math.max(...values) : 0
  const label = t(`admin.system.ops.${tile.label}`)
  return {
    key: tile.key,
    label,
    values,
    now: tile.format(now),
    warn: !!tile.warn && now > 0,
    summary: t('admin.system.ops.chart', { label, now: tile.format(now), peak: tile.format(peak) }),
  }
}))

const totals = computed(() => {
  const o = ops.value
  const tt = o.totals
  const p = o.process
  const poolWaited = o.db_pool.waited
  return [
    { key: 'requests', value: int.value.format(tt.requests) },
    { key: 'errors4xx', value: int.value.format(tt.errors_4xx) },
    { key: 'errors5xx', value: int.value.format(tt.errors_5xx), warn: tt.errors_5xx > 0 },
    { key: 'rateLimited', value: int.value.format(tt.rate_limited) },
    { key: 'wsConnections', value: int.value.format(tt.ws_connections) },
    { key: 'wsDropped', value: int.value.format(tt.ws_dropped), warn: tt.ws_dropped > 0 },
    { key: 'iceConnected', value: int.value.format(tt.ice_connected) },
    { key: 'iceFailed', value: int.value.format(tt.ice_failed), warn: tt.ice_failed > 0 },
    { key: 'iceDisconnected', value: int.value.format(tt.ice_disconnected) },
    { key: 'nack', value: int.value.format(tt.nack_requests) },
    { key: 'keyframes', value: int.value.format(tt.keyframe_requests) },
    { key: 'layerSwitches', value: int.value.format(tt.layer_switches) },
    { key: 'mlsRelayed', value: int.value.format(tt.mls_handshakes_relayed) },
    { key: 'mlsRejected', value: int.value.format(tt.mls_handshakes_rejected) },
    { key: 'storageErrors', value: `${int.value.format(tt.storage_errors)} / ${int.value.format(tt.storage_operations)}`, warn: tt.storage_errors > 0 },
    { key: 'traffic', value: `${bytes(tt.egress_bytes)} / ${bytes(tt.ingress_bytes)}` },
    { key: 'dbWaited', value: int.value.format(poolWaited) },
    { key: 'gc', value: `${int.value.format(p.gc_cycles)} (${nf.value.format(p.gc_pause_ms)} ms)` },
  ]
})
</script>

<template>
  <section class="space-y-3" data-testid="system-ops">
    <div class="space-y-1">
      <h4 class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.system.ops.heading') }}</h4>
      <p class="text-xs text-mnema-muted">{{ $t('admin.system.ops.hint') }}</p>
    </div>

    <p v-if="failed && !loaded" class="text-sm text-mnema-muted" data-testid="ops-unavailable">{{ $t('admin.system.ops.unavailable') }}</p>

    <template v-if="loaded">
      <p v-if="history.length < 2" class="text-xs text-mnema-tertiary" data-testid="ops-collecting">{{ $t('admin.system.ops.collecting') }}</p>
      <div class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-4">
        <div
          v-for="c in cards"
          :key="c.key"
          class="space-y-1.5 rounded-lg border border-mnema-hairline bg-mnema-surface p-3"
          :data-testid="`ops-tile-${c.key}`"
        >
          <div class="truncate text-xs text-mnema-tertiary" :title="c.label">{{ c.label }}</div>
          <div class="text-lg font-semibold tabular-nums" :class="c.warn ? 'text-mnema-warning' : 'text-mnema-text'" data-testid="ops-value">{{ c.now }}</div>
          <Sparkline :values="c.values" :label="c.summary" />
        </div>
      </div>

      <div class="space-y-2 rounded-lg border border-mnema-hairline bg-mnema-surface p-4">
        <div class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.system.ops.sinceStart') }}</div>
        <dl class="grid grid-cols-1 gap-x-6 gap-y-1 text-xs sm:grid-cols-2" data-testid="ops-totals">
          <div v-for="row in totals" :key="row.key" class="flex items-baseline justify-between gap-3">
            <dt class="text-mnema-tertiary">{{ $t(`admin.system.ops.total.${row.key}`) }}</dt>
            <dd class="tabular-nums" :class="row.warn ? 'text-mnema-warning' : 'text-mnema-text'">{{ row.value }}</dd>
          </div>
        </dl>
      </div>
    </template>
  </section>
</template>
