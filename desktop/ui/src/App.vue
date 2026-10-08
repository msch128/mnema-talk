<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import thirdPartyNotices from '../../THIRD_PARTY_NOTICES.md?raw'

interface Candidate { origin: string; community_id: string; api_version: number; login_available: boolean }
interface Game { platform: string; game: string | null; covers_monitor: boolean; exclusive_fullscreen_verified: boolean }
const address = ref('')
const busy = ref(false)
const candidate = ref<Candidate | null>(null)
const error = ref('')
let generation = 0
const games = ref('Wow.exe, GenshinImpact.exe')
const snapshot = ref<Game | null>(null)
const lastGame = ref<Game | null>(null)
const watching = ref(false)
const gameError = ref('')
const names = computed(() => games.value.split(',').map(value => value.trim()).filter(Boolean))
const invalidGames = computed(() => names.value.length === 0 || names.value.length > 32 || names.value.some(name =>
  name.length > 128 || name.includes('..') || !/^[a-zA-Z0-9][a-zA-Z0-9_. -]*\.exe$/i.test(name)))
let timer: ReturnType<typeof setInterval> | undefined
let gameGeneration = 0
let polling = false
const mediaHooks = {
  webRtc: 'RTCPeerConnection' in globalThis,
  capture: typeof navigator.mediaDevices?.getDisplayMedia === 'function',
  frameTransform: 'RTCRtpScriptTransform' in globalThis
}
const gameStatus = computed(() => snapshot.value?.platform === 'unsupported_probe_host'
  ? 'Diese Spielprüfung benötigt Windows.' : snapshot.value?.game ?? 'Kein freigegebenes Spiel im Vordergrund.')
watch(address, () => { candidate.value = null; error.value = '' })

async function inspectServer() {
  if (busy.value) return
  if (!address.value.trim() || address.value.length > 2048) {
    error.value = 'Bitte eine Serveradresse mit höchstens 2048 Zeichen eingeben.'
    return
  }
  const own = ++generation
  busy.value = true; candidate.value = null; error.value = ''
  try {
    const result = await invoke<Candidate>('inspect_server', { address: address.value })
    if (own === generation) candidate.value = result
  } catch {
    if (own === generation) error.value = 'Keine kompatible, über HTTPS erreichbare Mnema-Discovery. Adresse, Zertifikat und Serverversion prüfen.'
  } finally {
    if (own === generation) busy.value = false
  }
}

async function pollGame() {
  if (polling || !watching.value) return
  const own = gameGeneration
  polling = true
  try {
    const result = await invoke<Game>('inspect_game', { names: names.value })
    if (own === gameGeneration && watching.value) {
      snapshot.value = result
      gameError.value = ''
      if (result.game) lastGame.value = result
      if (result.platform === 'unsupported_probe_host') {
        watching.value = false
        clearInterval(timer)
        timer = undefined
      }
    }
  } catch {
    if (own === gameGeneration && watching.value) {
      snapshot.value = null
      lastGame.value = null
      gameError.value = 'Die lokale Spielprüfung ist fehlgeschlagen. Prüfung stoppen und erneut starten.'
    }
  } finally { polling = false }
}

function toggleWatching() {
  if (!watching.value && invalidGames.value) return
  gameGeneration++
  watching.value = !watching.value
  if (timer !== undefined) clearInterval(timer)
  timer = undefined
  snapshot.value = null
  lastGame.value = null
  gameError.value = ''
  if (watching.value) {
    timer = setInterval(() => { void pollGame() }, 1000)
    void pollGame()
  }
}

onUnmounted(() => {
  generation++; gameGeneration++; watching.value = false
  if (timer !== undefined) clearInterval(timer)
})
</script>

<template>
  <main>
    <header><span class="brand-mark" aria-hidden="true">M</span><h1>Mnema Talk</h1><span class="badge">Desktop · Entwicklung</span></header>
    <section aria-labelledby="connect-title">
      <h2 id="connect-title">Community prüfen</h2>
      <form @submit.prevent="inspectServer">
        <label for="server">Serveradresse</label>
        <div class="row"><input id="server" v-model="address" type="text" placeholder="https://talk.example.com" autocomplete="off" spellcheck="false" maxlength="2048" :disabled="busy"><button :disabled="busy || !address.trim()">{{ busy ? 'Prüft …' : 'Verbindung prüfen' }}</button></div>
      </form>
      <p v-if="error" role="alert">{{ error }}</p>
      <dl v-if="candidate"><dt>Zielserver</dt><dd>{{ candidate.origin }}</dd><dt>Community-ID</dt><dd>{{ candidate.community_id }}</dd><dt>API-Vertrag</dt><dd>{{ candidate.api_version }}</dd></dl>
      <p class="help">Nur öffentliche Serverdaten. Anmeldung und Voice benötigen erst ein geprüftes Sitzungs- und E2EE-Verfahren.</p>
    </section>
    <section aria-labelledby="game-title">
      <h2 id="game-title">Aktives Spiel erkennen</h2>
      <label for="games">Programmdateien, durch Komma getrennt</label>
      <div class="row"><input id="games" v-model="games" :disabled="watching" spellcheck="false" :aria-invalid="invalidGames" :aria-describedby="invalidGames ? 'games-validation' : undefined"><button type="button" :disabled="!watching && invalidGames" @click="toggleWatching">{{ watching ? 'Prüfung stoppen' : 'Spielprüfung starten' }}</button></div>
      <p v-if="invalidGames" id="games-validation" role="alert">Bitte 1 bis 32 Programmdateien mit .exe-Endung eingeben. Beginn mit Buchstabe oder Zahl, danach auch Leerzeichen, Punkt, Minus oder Unterstrich; keine Pfade oder aufeinanderfolgenden Punkte. Höchstens 128 Zeichen pro Name.</p>
      <p v-if="gameError" role="alert">{{ gameError }}</p>
      <p class="status" aria-live="polite">{{ gameStatus }}</p>
      <p v-if="lastGame" class="help">Zuletzt lokal erkannt: {{ lastGame.game }}. Monitorfüllende Geometrie: {{ lastGame.covers_monitor ? 'ja' : 'nein' }}. Exklusives Vollbild: noch nicht nachgewiesen.</p>
      <p class="help">Nach dem Start zum Spiel wechseln, dann hier das Ergebnis prüfen. Nur das Programm im Vordergrund wird gelesen; Fenstername und vollständiger Dateipfad werden weder angezeigt noch übertragen.</p>
    </section>
    <section aria-labelledby="media-title">
      <h2 id="media-title">Laufzeit-Funktionen</h2>
      <ul class="runtime-list"><li>WebRTC-API <span>{{ mediaHooks.webRtc ? 'vorhanden' : 'fehlt' }}</span></li><li>Bildschirm-Capture-API <span>{{ mediaHooks.capture ? 'vorhanden' : 'fehlt' }}</span></li><li>Encoded-Frame-Transform-API <span>{{ mediaHooks.frameTransform ? 'vorhanden' : 'fehlt' }}</span></li></ul>
      <p class="help">API-Verfügbarkeit belegt weder Aufnahme, E2EE noch Hardware-Encoding. Dieser Client startet keine Aufnahme.</p>
    </section>
    <details><summary>Open-Source-Lizenzen und Abhängigkeiten</summary><pre>{{ thirdPartyNotices }}</pre></details>
    <footer>Probe, kein veröffentlichter 0.7-Client. Ohne Voice erscheinen weder Widget noch Overlay.</footer>
  </main>
</template>
