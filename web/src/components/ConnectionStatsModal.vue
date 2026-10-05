<script setup>
import { computed } from 'vue'
import { Activity, Zap, ShieldCheck } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { rateJitter } from '../lib/rtcStats'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const emit = defineEmits(['close'])
const voiceStore = useVoiceStore()

const stats = computed(() => voiceStore.rtcStats)
const dash = value => (value == null || value === '' ? '–' : value)

// Sparkline of the measured WebSocket round trips.
const sparklinePoints = computed(() => {
  const history = voiceStore.pingHistory
  if (!history || history.length < 2) return ''
  const width = 280
  const height = 50
  const min = Math.max(0, Math.min(...history) - 2)
  const max = Math.max(...history, min + 5)
  const range = max - min || 1
  return history
    .map((val, idx) => {
      const x = (idx / (history.length - 1)) * width
      const y = height - ((val - min) / range) * (height - 8) - 4
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
})

const rows = computed(() => {
  const s = stats.value
  return [
    [t('stats.codec'), dash(s?.codec)],
    [t('stats.sendRecv'), s?.sendKbps != null ? `${s.sendKbps} / ${s.recvKbps} kbit/s` : '–'],
    [t('stats.packets'), s ? `${s.packetsSent} / ${s.packetsReceived}` : '–'],
    [t('stats.localCandidate'), dash(s?.localCandidate)],
    [t('stats.remoteCandidate'), dash(s?.remoteCandidate)],
    [t('stats.encryption'), s?.srtpCipher ? `DTLS-SRTP (${s.srtpCipher})` : 'DTLS-SRTP'],
    [t('stats.signalLatency'), voiceStore.ping != null ? `${voiceStore.ping} ms` : '–']
  ]
})
</script>

<template>
  <BaseDialog
    :title="$t('stats.title')"
    :subtitle="$t('stats.subtitle')"
    panel-class="max-w-lg select-none"
    @close="emit('close')"
  >
    <template #badge>
      <span
        :class="[
          'flex flex-shrink-0 items-center gap-1 rounded-full px-1.5 py-0.5 font-mono text-xs font-medium',
          stats?.connected ? 'bg-mnema-accent/15 text-mnema-accent' : 'bg-mnema-surface text-mnema-tertiary'
        ]"
      >
        <span :class="['h-1.5 w-1.5 rounded-full', stats?.connected ? 'bg-mnema-accent' : 'bg-mnema-tertiary']"></span>
        {{ stats?.connected ? $t('stats.connected') : $t('stats.noConnection') }}
      </span>
    </template>

    <div class="p-5 space-y-5 overflow-y-auto max-h-[80vh]">
      <div class="grid grid-cols-3 gap-2.5">
        <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline min-w-0">
          <div class="flex items-center justify-between gap-1 text-mnema-tertiary text-xs uppercase font-mono">
            <span class="truncate">{{ $t('stats.latency') }}</span>
            <Activity class="w-4 h-4 text-mnema-accent flex-shrink-0" />
          </div>
          <div class="text-xl font-bold font-mono text-mnema-text mt-1 flex items-baseline gap-1">
            <span>{{ dash(stats?.rttMs) }}</span>
            <span class="text-sm font-normal text-mnema-tertiary">ms</span>
          </div>
          <div class="text-xs text-mnema-tertiary font-mono mt-1">{{ $t('stats.latencyHint') }}</div>
        </div>

        <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline min-w-0">
          <div class="flex items-center justify-between gap-1 text-mnema-tertiary text-xs uppercase font-mono">
            <span class="truncate">{{ $t('stats.jitterLabel') }}</span>
            <Zap class="w-4 h-4 text-mnema-mint flex-shrink-0" />
          </div>
          <div class="text-xl font-bold font-mono text-mnema-text mt-1 flex items-baseline gap-1">
            <span>{{ dash(stats?.jitterMs) }}</span>
            <span class="text-sm font-normal text-mnema-tertiary">ms</span>
          </div>
          <div class="text-xs text-mnema-tertiary font-mono mt-1">{{ rateJitter(stats?.jitterMs) }}</div>
        </div>

        <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline min-w-0">
          <div class="flex items-center justify-between gap-1 text-mnema-tertiary text-xs uppercase font-mono">
            <span class="truncate">{{ $t('stats.loss') }}</span>
            <ShieldCheck class="w-4 h-4 text-mnema-accent flex-shrink-0" />
          </div>
          <div class="text-xl font-bold font-mono text-mnema-text mt-1 flex items-baseline gap-1">
            <span>{{ stats ? stats.lossPercent : '–' }}</span>
            <span class="text-sm font-normal text-mnema-tertiary">%</span>
          </div>
          <div class="text-xs text-mnema-tertiary font-mono mt-1">
            {{ stats ? $t('stats.lost', { count: stats.packetsLost }) : $t('stats.noData') }}
          </div>
        </div>
      </div>

      <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline space-y-2">
        <div class="flex items-center justify-between gap-2 text-sm">
          <span class="text-xs font-medium text-mnema-text">{{ $t('stats.signalHistory') }}</span>
          <span class="text-xs text-mnema-tertiary font-mono">
            {{ $t('stats.summary', { avg: dash(voiceStore.avgPing), min: dash(voiceStore.minPing), max: dash(voiceStore.maxPing) }) }}
          </span>
        </div>
        <div class="h-16 w-full bg-mnema-canvas rounded border border-mnema-border p-2 relative overflow-hidden flex items-end">
          <svg v-if="sparklinePoints" viewBox="0 0 280 50" preserveAspectRatio="none" class="w-full h-12 overflow-visible" role="img" :aria-label="$t('stats.signalHistory')">
            <polyline
              fill="none"
              stroke="#2DA771"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
              :points="sparklinePoints"
            />
          </svg>
          <span v-else class="text-xs text-mnema-tertiary m-auto">{{ $t('stats.noSamples') }}</span>
        </div>
      </div>

      <div class="space-y-1.5">
        <h3 class="text-xs uppercase font-mono tracking-wider text-mnema-tertiary font-semibold">{{ $t('stats.details') }}</h3>
        <div class="rounded-lg bg-mnema-surface border border-mnema-hairline divide-y divide-mnema-hairline text-sm">
          <div v-for="[label, value] in rows" :key="label" class="px-3 py-2 flex items-center justify-between gap-4">
            <span class="text-mnema-tertiary">{{ label }}</span>
            <span class="font-mono text-mnema-text text-right">{{ value }}</span>
          </div>
        </div>
        <p class="text-xs text-mnema-tertiary leading-relaxed">
          {{ $t('stats.privacyNote') }}
        </p>
      </div>
    </div>

    <footer class="px-5 py-3 border-t border-mnema-hairline bg-mnema-raised flex items-center justify-end">
      <button
        type="button"
        class="px-4 py-1.5 rounded-md bg-mnema-elevated border border-mnema-border hover:bg-mnema-hover text-mnema-text text-sm font-semibold transition"
        @click="emit('close')"
      >
        {{ $t('common.close') }}
      </button>
    </footer>
  </BaseDialog>
</template>
