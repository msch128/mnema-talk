<script setup>
import { computed } from 'vue'
import { 
  X, Activity, Radio, Cpu, ShieldCheck, Zap, 
  Volume2, Sliders, CheckCircle2, Sparkles, RefreshCw
} from 'lucide-vue-next'
import { useVoiceStore } from '../stores/voice'

const emit = defineEmits(['close'])
const voiceStore = useVoiceStore()

// SVG Sparkline calculation for live ping history
const sparklinePoints = computed(() => {
  const history = voiceStore.pingHistory
  if (!history || history.length < 2) return ''

  const width = 280
  const height = 50
  const min = Math.max(1, Math.min(...history) - 2)
  const max = Math.max(...history, min + 5)
  const range = max - min || 1

  return history.map((val, idx) => {
    const x = (idx / (history.length - 1)) * width
    const y = height - ((val - min) / range) * (height - 8) - 4
    return `${x.toFixed(1)},${y.toFixed(1)}`
  }).join(' ')
})
</script>

<template>
  <div class="fixed inset-0 bg-black/75 backdrop-blur-sm z-50 flex items-center justify-center p-4 select-none">
    <div class="bg-mnema-elevated w-full max-w-lg rounded-xl flex flex-col shadow-2xl border border-mnema-border overflow-hidden">
      <!-- Modal Header -->
      <header class="px-5 py-4 border-b border-mnema-hairline flex items-center justify-between bg-mnema-raised">
        <div class="flex items-center gap-2.5">
          <div class="w-7 h-7 rounded-md bg-mnema-accent/15 border border-mnema-accent/30 flex items-center justify-center text-mnema-accent">
            <Radio class="w-4 h-4" />
          </div>
          <div>
            <div class="flex items-center gap-2">
              <h2 class="text-xs font-semibold text-mnema-text">RTC Sprach- & Verbindungsstatus</h2>
              <span class="text-[10px] px-1.5 py-0.5 rounded-full bg-mnema-accent/15 text-mnema-accent font-medium flex items-center gap-1 font-mono">
                <span class="w-1.5 h-1.5 rounded-full bg-mnema-accent animate-pulse"></span>
                Verbunden
              </span>
            </div>
            <p class="text-[10px] text-mnema-tertiary font-mono">Echtzeit WebRTC Pion SFU Telemetrie</p>
          </div>
        </div>

        <button 
          @click="emit('close')"
          class="p-1.5 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
        >
          <X class="w-4 h-4" />
        </button>
      </header>

      <!-- Modal Body -->
      <div class="p-5 space-y-5 overflow-y-auto max-h-[80vh]">
        <!-- Primary KPI Grid -->
        <div class="grid grid-cols-3 gap-2.5">
          <!-- Ping KPI Card -->
          <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline">
            <div class="flex items-center justify-between text-mnema-tertiary text-[10px] uppercase font-mono">
              <span>Latenz (RTT)</span>
              <Activity class="w-3.5 h-3.5 text-mnema-accent" />
            </div>
            <div class="text-xl font-bold font-mono text-mnema-text mt-1 flex items-baseline gap-1">
              <span>{{ voiceStore.ping }}</span>
              <span class="text-xs font-normal text-mnema-tertiary">ms</span>
            </div>
            <div class="text-[9px] text-mnema-tertiary font-mono mt-1">
              Min: {{ voiceStore.minPing }}ms • Max: {{ voiceStore.maxPing }}ms
            </div>
          </div>

          <!-- Jitter KPI Card -->
          <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline">
            <div class="flex items-center justify-between text-mnema-tertiary text-[10px] uppercase font-mono">
              <span>Jitter</span>
              <Zap class="w-3.5 h-3.5 text-mnema-mint" />
            </div>
            <div class="text-xl font-bold font-mono text-mnema-text mt-1 flex items-baseline gap-1">
              <span>{{ voiceStore.jitter }}</span>
              <span class="text-xs font-normal text-mnema-tertiary">ms</span>
            </div>
            <div class="text-[9px] text-mnema-accent font-mono mt-1">
              Hervorragend (&lt;5ms)
            </div>
          </div>

          <!-- Packet Loss KPI Card -->
          <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline">
            <div class="flex items-center justify-between text-mnema-tertiary text-[10px] uppercase font-mono">
              <span>Paketverlust</span>
              <ShieldCheck class="w-3.5 h-3.5 text-mnema-accent" />
            </div>
            <div class="text-xl font-bold font-mono text-mnema-text mt-1 flex items-baseline gap-1">
              <span>{{ voiceStore.packetLossPercent }}</span>
              <span class="text-xs font-normal text-mnema-tertiary">%</span>
            </div>
            <div class="text-[9px] text-mnema-mint font-mono mt-1">
              0 verworfen
            </div>
          </div>
        </div>

        <!-- Live Ping Sparkline Graph -->
        <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline space-y-2">
          <div class="flex items-center justify-between text-xs">
            <span class="text-[11px] font-medium text-mnema-text">Echtzeit-Latenzverlauf (letzte 30 Pings)</span>
            <span class="text-[10px] text-mnema-tertiary font-mono">Avg: {{ voiceStore.avgPing }} ms</span>
          </div>

          <!-- Waveform container -->
          <div class="h-16 w-full bg-mnema-canvas rounded border border-mnema-border p-2 relative overflow-hidden flex items-end">
            <!-- Background grid lines -->
            <div class="absolute inset-0 flex flex-col justify-between p-1 opacity-10 pointer-events-none">
              <div class="border-b border-mnema-text w-full"></div>
              <div class="border-b border-mnema-text w-full"></div>
              <div class="border-b border-mnema-text w-full"></div>
            </div>

            <svg viewBox="0 0 280 50" preserveAspectRatio="none" class="w-full h-12 overflow-visible">
              <polyline
                fill="none"
                stroke="#2DA771"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                :points="sparklinePoints"
              />
            </svg>
          </div>
        </div>

        <!-- AI Noise Cancelling (Krisp Equivalent) Setting Card -->
        <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline flex items-center justify-between">
          <div class="flex items-center gap-3">
            <div class="w-8 h-8 rounded-lg bg-mnema-accent/15 border border-mnema-accent/30 flex items-center justify-center text-mnema-accent flex-shrink-0">
              <Sparkles class="w-4 h-4" />
            </div>
            <div>
              <div class="text-xs font-semibold text-mnema-text flex items-center gap-1.5">
                <span>KI Rauschunterdrückung</span>
                <span class="text-[9px] px-1 rounded bg-mnema-mint/20 text-mnema-mint font-mono">RNNoise Neural AI</span>
              </div>
              <p class="text-[10px] text-mnema-tertiary">
                Tastaturklappern, Hintergrundlärm und Echos werden in Echtzeit gefiltert (Krisp-Pendant).
              </p>
            </div>
          </div>

          <button
            @click="voiceStore.toggleNoiseCancelling"
            :class="[
              'w-11 h-6 rounded-full transition-colors relative flex items-center px-0.5',
              voiceStore.noiseCancelling ? 'bg-mnema-accent' : 'bg-mnema-canvas border border-mnema-border'
            ]"
          >
            <div 
              :class="[
                'w-5 h-5 rounded-full bg-white transition-transform shadow-sm',
                voiceStore.noiseCancelling ? 'translate-x-5' : 'translate-x-0'
              ]"
            ></div>
          </button>
        </div>

        <!-- Technical Diagnostics (Discord-Style Details) -->
        <div class="space-y-1.5">
          <h3 class="text-[10px] uppercase font-mono tracking-wider text-mnema-tertiary font-semibold">
            Audio & RTC Parameter
          </h3>
          <div class="rounded-lg bg-mnema-surface border border-mnema-hairline divide-y divide-mnema-hairline text-xs">
            <div class="px-3 py-2 flex items-center justify-between">
              <span class="text-mnema-tertiary">Audio-Codec</span>
              <span class="font-mono text-mnema-text">Opus 48.000 Hz, 2 Kanäle (128 kbps VBR)</span>
            </div>
            <div class="px-3 py-2 flex items-center justify-between">
              <span class="text-mnema-tertiary">Transport-Protokoll</span>
              <span class="font-mono text-mnema-text">WebRTC UDP (Pion SFU Media Engine)</span>
            </div>
            <div class="px-3 py-2 flex items-center justify-between">
              <span class="text-mnema-tertiary">ICE Verbindungstyp</span>
              <span class="font-mono text-mnema-text">Host / STUN Reflexive (srflx)</span>
            </div>
            <div class="px-3 py-2 flex items-center justify-between">
              <span class="text-mnema-tertiary">Pakete Gesendet / Empfangen</span>
              <span class="font-mono text-mnema-text">{{ voiceStore.packetsSent }} / {{ voiceStore.packetsReceived }}</span>
            </div>
            <div class="px-3 py-2 flex items-center justify-between">
              <span class="text-mnema-tertiary">Verschlüsselung</span>
              <span class="font-mono text-mnema-text">E2E DTLS-SRTP (AEAD AES-128-GCM)</span>
            </div>
            <div class="px-3 py-2 flex items-center justify-between">
              <span class="text-mnema-tertiary">Server-Knoten</span>
              <span class="font-mono text-mnema-text">Mnema Private Cloud Core</span>
            </div>
          </div>
        </div>
      </div>

      <!-- Footer -->
      <footer class="px-5 py-3 border-t border-mnema-hairline bg-mnema-raised flex items-center justify-end">
        <button
          @click="emit('close')"
          class="px-4 py-1.5 rounded-lg bg-mnema-surface border border-mnema-border hover:bg-mnema-hover text-mnema-text text-xs transition"
        >
          Schließen
        </button>
      </footer>
    </div>
  </div>
</template>
