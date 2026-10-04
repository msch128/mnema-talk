<script setup>
import { computed } from 'vue'
import { X, Activity, Radio, Zap, ShieldCheck } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { rateJitter } from '../lib/rtcStats'

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
    ['Audio-Codec', dash(s?.codec)],
    ['Senden / Empfangen', s?.sendKbps != null ? `${s.sendKbps} / ${s.recvKbps} kbit/s` : '–'],
    ['Pakete gesendet / empfangen', s ? `${s.packetsSent} / ${s.packetsReceived}` : '–'],
    ['Lokaler Kandidat', dash(s?.localCandidate)],
    ['Server-Kandidat', dash(s?.remoteCandidate)],
    ['Transportverschlüsselung', s?.srtpCipher ? `DTLS-SRTP (${s.srtpCipher})` : 'DTLS-SRTP'],
    ['Signal-Latenz (WebSocket)', voiceStore.ping != null ? `${voiceStore.ping} ms` : '–']
  ]
})
</script>

<template>
  <div class="fixed inset-0 bg-black/80 z-50 flex items-center justify-center p-4 select-none">
    <div class="bg-mnema-elevated w-full max-w-lg rounded-xl flex flex-col shadow-2xl border border-mnema-border overflow-hidden">
      <header class="px-5 py-4 border-b border-mnema-hairline flex items-center justify-between bg-mnema-raised">
        <div class="flex items-center gap-2.5">
          <div class="w-7 h-7 rounded-md bg-mnema-accent/15 border border-mnema-accent/30 flex items-center justify-center text-mnema-accent">
            <Radio class="w-4 h-4" />
          </div>
          <div>
            <div class="flex items-center gap-2">
              <h2 class="text-lg font-semibold text-mnema-text">Verbindungsstatus</h2>
              <span
                :class="[
                  'text-xs px-1.5 py-0.5 rounded-full font-medium flex items-center gap-1 font-mono',
                  stats?.connected ? 'bg-mnema-accent/15 text-mnema-accent' : 'bg-mnema-surface text-mnema-tertiary'
                ]"
              >
                <span :class="['w-1.5 h-1.5 rounded-full', stats?.connected ? 'bg-mnema-accent' : 'bg-mnema-tertiary']"></span>
                {{ stats?.connected ? 'Verbunden' : 'Keine Sprachverbindung' }}
              </span>
            </div>
            <p class="text-xs text-mnema-tertiary font-mono">Gemessen vom Browser (WebRTC getStats)</p>
          </div>
        </div>
        <button
          class="p-1.5 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
          title="Schließen"
          @click="emit('close')"
        >
          <X class="w-4 h-4" />
        </button>
      </header>

      <div class="p-5 space-y-5 overflow-y-auto max-h-[80vh]">
        <div class="grid grid-cols-3 gap-2.5">
          <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline">
            <div class="flex items-center justify-between text-mnema-tertiary text-xs uppercase font-mono">
              <span>Latenz (RTT)</span>
              <Activity class="w-4 h-4 text-mnema-accent" />
            </div>
            <div class="text-xl font-bold font-mono text-mnema-text mt-1 flex items-baseline gap-1">
              <span>{{ dash(stats?.rttMs) }}</span>
              <span class="text-sm font-normal text-mnema-tertiary">ms</span>
            </div>
            <div class="text-xs text-mnema-tertiary font-mono mt-1">Sprachverbindung zum Server</div>
          </div>

          <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline">
            <div class="flex items-center justify-between text-mnema-tertiary text-xs uppercase font-mono">
              <span>Jitter</span>
              <Zap class="w-4 h-4 text-mnema-mint" />
            </div>
            <div class="text-xl font-bold font-mono text-mnema-text mt-1 flex items-baseline gap-1">
              <span>{{ dash(stats?.jitterMs) }}</span>
              <span class="text-sm font-normal text-mnema-tertiary">ms</span>
            </div>
            <div class="text-xs text-mnema-tertiary font-mono mt-1">{{ rateJitter(stats?.jitterMs) }}</div>
          </div>

          <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline">
            <div class="flex items-center justify-between text-mnema-tertiary text-xs uppercase font-mono">
              <span>Paketverlust</span>
              <ShieldCheck class="w-4 h-4 text-mnema-accent" />
            </div>
            <div class="text-xl font-bold font-mono text-mnema-text mt-1 flex items-baseline gap-1">
              <span>{{ stats ? stats.lossPercent : '–' }}</span>
              <span class="text-sm font-normal text-mnema-tertiary">%</span>
            </div>
            <div class="text-xs text-mnema-tertiary font-mono mt-1">
              {{ stats ? `${stats.packetsLost} verloren` : 'keine Daten' }}
            </div>
          </div>
        </div>

        <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline space-y-2">
          <div class="flex items-center justify-between text-sm">
            <span class="text-xs font-medium text-mnema-text">Signal-Latenz (letzte 30 Messungen)</span>
            <span class="text-xs text-mnema-tertiary font-mono">
              Ø {{ dash(voiceStore.avgPing) }} ms · min {{ dash(voiceStore.minPing) }} · max {{ dash(voiceStore.maxPing) }}
            </span>
          </div>
          <div class="h-16 w-full bg-mnema-canvas rounded border border-mnema-border p-2 relative overflow-hidden flex items-end">
            <svg v-if="sparklinePoints" viewBox="0 0 280 50" preserveAspectRatio="none" class="w-full h-12 overflow-visible">
              <polyline
                fill="none"
                stroke="#2DA771"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                :points="sparklinePoints"
              />
            </svg>
            <span v-else class="text-xs text-mnema-tertiary m-auto">Noch keine Messwerte</span>
          </div>
        </div>

        <div class="space-y-1.5">
          <h3 class="text-xs uppercase font-mono tracking-wider text-mnema-tertiary font-semibold">Details</h3>
          <div class="rounded-lg bg-mnema-surface border border-mnema-hairline divide-y divide-mnema-hairline text-sm">
            <div v-for="[label, value] in rows" :key="label" class="px-3 py-2 flex items-center justify-between gap-4">
              <span class="text-mnema-tertiary">{{ label }}</span>
              <span class="font-mono text-mnema-text text-right">{{ value }}</span>
            </div>
          </div>
          <p class="text-xs text-mnema-tertiary leading-relaxed">
            Sprache und Bildschirm sind auf dem Weg zum Server verschlüsselt. Der Server (SFU) entschlüsselt die
            Pakete zur Weiterleitung an die anderen Teilnehmer; es gibt keine Ende-zu-Ende-Verschlüsselung und
            keine Aufzeichnung.
          </p>
        </div>
      </div>

      <footer class="px-5 py-3 border-t border-mnema-hairline bg-mnema-raised flex items-center justify-end">
        <button
          class="px-4 py-1.5 rounded-lg bg-mnema-surface border border-mnema-border hover:bg-mnema-hover text-mnema-text text-sm transition"
          @click="emit('close')"
        >
          Schließen
        </button>
      </footer>
    </div>
  </div>
</template>
