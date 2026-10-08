<script setup lang="ts">
import { computed, onUnmounted, ref, toRaw } from 'vue'
import { createNativeMediaBridge } from './native-media-invoke'
import { emptyReport, SyntheticLoopback, type Report } from './loopback'
const canvas = ref<HTMLCanvasElement>()
const video = ref<HTMLVideoElement>()
const report = ref<Report>(emptyReport(0, true))
report.value.phase = 'idle'
const previous = ref<Report[]>([])
const nativeBridge = createNativeMediaBridge()
let session: SyntheticLoopback | undefined
let run = 0
const running = computed(() => !['idle', 'stopped'].includes(report.value.phase))
async function start(mode: 'forward' | 'tamper' | 'revoke' | 'close') {
  await session?.stop()
  if (report.value.run) previous.value = [structuredClone(toRaw(report.value)), ...previous.value].slice(0, 4)
  if (!canvas.value || !video.value) return
  session = new SyntheticLoopback(canvas.value, video.value, true, ++run, value => { report.value = value }, true, mode, nativeBridge)
  report.value = emptyReport(run, true)
  await session.start()
}
async function stop() { await session?.stop() }
onUnmounted(() => { void session?.stop() })
</script>
<template>
  <main>
    <header><span class="mark">M</span><h1>Mnema Talk</h1><span>Nativer authentifizierter SFU-Test</span></header>
    <p>Zwei native Testkonten im WebView, echter authentifizierter Go/Pion-SFU-Pfad auf Loopback. Nur synthetisches Video und 440-Hz-Audio; lokale zufällige Fixture-Schlüssel. Keine Community-Zugangsdaten, keine physische Aufnahme oder Speicherung.</p>
    <div class="buttons"><button :disabled="running" @click="start('forward')">SFU-Datenfluss</button><button :disabled="running" @click="start('tamper')">Manipulation prüfen</button><button :disabled="running" @click="start('close')">Publisher-Transport schließen</button><button :disabled="!running" @click="stop">Stoppen</button></div>
    <p class="status">Lauf {{ report.run }} · {{ report.fixtureMode }} · {{ report.phase }} · {{ report.seconds }} s · Datenfluss {{ report.passedSyntheticFlow ? 'nachgewiesen' : 'nicht nachgewiesen' }} · Fehlerfall {{ report.passedFaultScenario ? 'nachgewiesen' : 'nicht nachgewiesen' }}</p>
    <div class="previews"><div><label>Synthetische Quelle</label><canvas ref="canvas"></canvas></div><div><label>Über SFU empfangen</label><video ref="video" autoplay muted playsinline></video></div></div>
    <section><h2>Messwerte</h2><pre>{{ JSON.stringify(report, null, 2) }}</pre></section>
    <details v-if="previous.length"><summary>Vorherige Läufe</summary><pre>{{ JSON.stringify(previous, null, 2) }}</pre></details>
    <footer>Entwickler-Fixture mit nativen Testkonten und sitzungsgebundener Signalisierung. Die native Integration wird separat qualifiziert; lokale Fixture-Schlüssel sind keine MLS-Mitgliedsschlüssel. Ein manipulierter Frame wird nach Verschlüsselung verändert und muss authentifiziert abgewiesen werden; diese Messung bleibt als Fehler erhalten. Entfernung prüft ausschließlich Transport-/SFU-Mitgliedschaft, keine MLS-Schlüsselwiderrufung. Keine Produktions-E2EE-, Gerätevertrauens-, Persistenz-, Plattform- oder Gaming-Zusage. SDP, ICE-Kandidaten, Frames und Schlüssel werden nicht angezeigt oder aufgezeichnet. Audioausgabe bleibt stumm.</footer>
  </main>
</template>
