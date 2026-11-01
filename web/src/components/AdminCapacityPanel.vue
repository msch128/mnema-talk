<script setup lang="ts">
// Capacity panel of Admin > Status: live CPU, memory and media rates, and an estimate of how many more members
// the server can take (GET /api/admin/status/capacity). The estimate is labelled as one, with its assumptions.
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { Gauge } from '@lucide/vue'
import { api } from '../lib/api'
import { formatFileSize } from '../lib/fileSize'
import { decodeServerCapacityStatus, type ServerCapacityStatus } from '../types/rest'
import { locale, t } from '../i18n'

const REFRESH_MS = 10_000

const cap = ref<ServerCapacityStatus | null>(null)
let timer: ReturnType<typeof setInterval> | undefined

async function load() {
  try {
    cap.value = await api('/api/admin/status/capacity', { decode: decodeServerCapacityStatus })
  } catch {
    // Advisory: the last figures stay.
  }
}

onMounted(() => {
  void load()
  timer = setInterval(() => void load(), REFRESH_MS)
})
onUnmounted(() => clearInterval(timer))

const num = (v: number, digits = 1) => new Intl.NumberFormat(locale.value, { maximumFractionDigits: digits }).format(v)
const size = (bytes: number) => formatFileSize(bytes, locale.value)

// Everything the template shows, worked out once from the last answer.
const view = computed(() => {
  const c = cap.value
  if (!c) return null
  const e = c.estimate
  const p = 'admin.operatorStatus.capacity.'
  const basis = e.basis === 'observed' ? t(`${p}basisObserved`, { scale: num(e.scale, 2) }) : t(`${p}basisTable`)
  return {
    status: e.status,
    cpu: t(`${p}cpuValue`, { used: num(c.cpu_used_cores, 2), total: num(c.cpu_cores, 2) }),
    memory: c.memory_limit_bytes > 0
      ? t(`${p}memoryValue`, { used: size(c.rss_bytes), limit: size(c.memory_limit_bytes) })
      : t(`${p}memoryNoLimit`, { used: size(c.rss_bytes) }),
    media: t(`${p}mediaValue`, { mbps: num(c.egress_mbps), pps: num(c.egress_pps, 0) }),
    calls: t(`${p}callsValue`, { peers: c.peers, shares: c.screen_shares }),
    verdict: e.status === 'ok' ? t(`${p}estimateOk`, { count: e.more_members }) : t(`${p}estimate_${e.status}`),
    assumption: t(`${p}assumption`, { cores: num(e.plan_cores, 2), cost: num(e.per_member_millicores, 1), basis })
  }
})
</script>

<template>
  <div v-if="view" class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3" data-testid="status-capacity">
    <div class="flex items-center gap-2">
      <Gauge class="w-4 h-4 text-mnema-accent shrink-0" aria-hidden="true" />
      <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.operatorStatus.capacity.heading') }}</span>
    </div>
    <dl class="grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
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
    <div class="pt-2 border-t border-mnema-hairline space-y-1">
      <p class="text-sm font-medium text-mnema-text" :data-status="view.status" data-testid="capacity-estimate">{{ view.verdict }}</p>
      <p class="text-xs text-mnema-muted" data-testid="capacity-assumption">{{ view.assumption }}</p>
    </div>
  </div>
</template>
