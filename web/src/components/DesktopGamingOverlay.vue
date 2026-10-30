<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, toRaw } from 'vue'
import { t } from '../i18n'
import { platform } from '../lib/platform'
import { Mic, MicOff, Headphones, HeadphoneOff, Monitor, X, Settings } from '@lucide/vue'
import SwitchRow from './SwitchRow.vue'
import GamingGameList from './GamingGameList.vue'
import { chordFromKey, chordText, type GamingSettings, type GamingSnapshot } from '../lib/desktopGaming'
const settingsOnly = platform.gamingSurface() === 'gaming-settings'
const interactive = settingsOnly || platform.gamingSurface() === 'gaming-overlay'
const snapshot = ref<GamingSnapshot | null>(null)
const labels = computed(() => ({ overlay: t('gaming.overlay'), mute: t('gaming.mute'), deafen: t('gaming.deafen'), ptt: t('gaming.ptt') }))
const editing = ref(settingsOnly)
const draft = ref<GamingSettings | null>(null)
const gameList = ref<InstanceType<typeof GamingGameList> | null>(null)
const error = ref(false)
// The last other app in front, offered by the host only when asked, for the person to confirm.
const candidate = ref<string | null>(null)
const addNote = ref<'none' | 'failed' | null>(null)
async function askCandidate() {
  addNote.value = null; candidate.value = null
  try { const name = await platform.invoke<string | null>('desktop_gaming_candidate'); if (name) candidate.value = name; else addNote.value = 'none' }
  catch { addNote.value = 'failed' }
}
async function addCandidate() {
  const name = candidate.value
  if (!name || !draft.value) return
  try { draft.value.games = await platform.invoke<string[]>('desktop_gaming_add_game', { name }); candidate.value = null; addNote.value = null }
  catch { candidate.value = null; addNote.value = 'failed' }
}
const members = computed(() => interactive ? snapshot.value?.voice.members ?? [] : (snapshot.value?.voice.members ?? []).slice(0, 12))
let timer: ReturnType<typeof setInterval> | undefined
let pending = false; let alive = true
async function refresh() {
  if (!alive || pending) return
  pending = true
  try { const next = await platform.invoke<GamingSnapshot>('desktop_gaming_snapshot'); if (alive) { snapshot.value = next; if (settingsOnly && !draft.value) draft.value = structuredClone(next.settings) } }
  catch { snapshot.value = null } finally { pending = false }
}
async function control(action: string) {
  try { await platform.invoke('desktop_gaming_control', { action }); error.value = false } catch { error.value = true }
}
function settings() { draft.value = snapshot.value ? structuredClone(toRaw(snapshot.value.settings)) : null; editing.value = !editing.value; error.value = false }
function bind(key: 'overlay' | 'mute' | 'deafen' | 'ptt', event: KeyboardEvent) {
  const chord = chordFromKey(event)
  // Tab and Escape without a chord must still leave the field.
  if (!chord && (event.key === 'Tab' || event.key === 'Escape')) return
  event.preventDefault(); event.stopPropagation()
  if (chord && draft.value) draft.value[key] = chord
}
async function save() {
  if (!draft.value) return
  if (gameList.value && !gameList.value.add()) return
  try { await platform.invoke('desktop_gaming_settings', { settings: draft.value }); editing.value = settingsOnly; error.value = false }
  catch { error.value = true }
}
onMounted(() => {
  document.documentElement.style.background = 'transparent'; document.body.style.background = 'transparent'
  void refresh(); timer = setInterval(() => { void refresh() }, 200)
})
onBeforeUnmount(() => { alive = false; clearInterval(timer) })
</script>
<template>
  <section v-if="snapshot && (snapshot.active || settingsOnly)" class="gaming-surface" :class="{ interactive }" :style="{ '--sidepeek-opacity': snapshot.settings.sidepeek_opacity / 100 }" :aria-label="$t('gaming.title')">
    <header class="flex items-center justify-between gap-2 mb-3">
      <strong>{{ settingsOnly ? $t('gaming.settings') : $t('gaming.title') }}</strong>
      <span v-if="!settingsOnly" class="text-xs text-mnema-muted">{{ snapshot.voice.members.length }}</span>
      <template v-if="interactive">
        <button v-if="!settingsOnly" :aria-label="$t('gaming.settings')" @click="settings"><Settings class="w-4 h-4" /></button>
        <button :aria-label="$t('common.close')" @click="control('close')"><X class="w-4 h-4" /></button>
      </template>
    </header>
    <div v-if="interactive && !settingsOnly" class="controls flex gap-2 mb-3">
      <button :aria-label="snapshot.voice.muted ? $t('voice.unmute') : $t('voice.mute')" :aria-pressed="snapshot.voice.muted" @click="control('mute')"><MicOff v-if="snapshot.voice.muted" /><Mic v-else /></button>
      <button :aria-label="snapshot.voice.deafened ? $t('voice.undeafen') : $t('voice.deafen')" :aria-pressed="snapshot.voice.deafened" @click="control('deafen')"><HeadphoneOff v-if="snapshot.voice.deafened" /><Headphones v-else /></button>
      <button :aria-label="snapshot.voice.sharing ? $t('voice.stopShare') : $t('voice.share')" :aria-pressed="snapshot.voice.sharing" @click="control('stream')"><Monitor /></button>
      <button :aria-pressed="snapshot.voice.ptt_mode" @click="control('ptt-mode')">{{ $t('gaming.ptt') }}</button>
    </div>
    <ul v-if="!settingsOnly" class="space-y-2 overflow-y-auto max-h-72">
      <li v-for="member in members" :key="member.id" class="flex items-center gap-2 rounded-md px-2 py-1" :class="{ speaking: member.speaking }">
        <span class="speaker-dot" :class="{ live: member.speaking }"></span>
        <span class="truncate flex-1">{{ member.name }}</span>
        <MicOff v-if="member.muted" class="w-4 h-4 text-mnema-danger" :aria-label="$t('voice.mute')" />
        <Monitor v-if="member.sharing" class="w-4 h-4 text-mnema-accent" :aria-label="$t('voice.share')" />
      </li>
    </ul>
    <p v-if="!interactive" class="mt-3 text-xs text-mnema-muted">{{ chordText(snapshot.settings.overlay) }}</p>
    <form v-if="interactive && editing && draft" class="gaming-form" @submit.prevent="save">
      <section class="settings-group" aria-labelledby="gaming-group-general">
        <h3 id="gaming-group-general" class="settings-label">{{ $t('gaming.groupGeneral') }}</h3>
        <div class="settings-card">
          <SwitchRow v-model="draft.enabled" :title="$t('gaming.enabled')" :hint="$t('gaming.enabledHint')" data-testid="gaming-enabled" />
          <div class="settings-sub">
            <label for="gaming-opacity" class="flex items-baseline justify-between gap-3">
              <span class="settings-row-title">{{ $t('gaming.opacity') }}</span>
              <output for="gaming-opacity" class="font-mono text-xs text-mnema-muted">{{ draft.sidepeek_opacity }}%</output>
            </label>
            <input id="gaming-opacity" v-model.number="draft.sidepeek_opacity" type="range" min="0" max="100" step="5" aria-describedby="gaming-opacity-hint" class="block w-full" />
            <p id="gaming-opacity-hint" class="settings-hint">{{ $t('gaming.opacityHint') }}</p>
          </div>
        </div>
      </section>
      <section class="settings-group" aria-labelledby="gaming-group-keys">
        <h3 id="gaming-group-keys" class="settings-label">{{ $t('gaming.groupShortcuts') }}</h3>
        <p class="settings-help">{{ $t('gaming.shortcutsHelp') }}</p>
        <div class="settings-rows">
          <label v-for="key in (['overlay', 'mute', 'deafen', 'ptt'] as const)" :key="key" class="settings-row">
            <span class="settings-row-title">{{ labels[key] }}</span>
            <input :aria-label="labels[key]" :value="chordText(draft[key])" readonly class="w-32 shrink-0 rounded-md border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-center font-mono text-sm text-mnema-text sm:w-40" @keydown="bind(key, $event)" />
          </label>
        </div>
      </section>
      <section class="settings-group" aria-labelledby="gaming-group-games">
        <h3 id="gaming-group-games" class="settings-label">{{ $t('gaming.groupGames') }}</h3>
        <p class="settings-help">{{ $t('gaming.gamesHint') }}</p>
        <div class="settings-card">
          <GamingGameList ref="gameList" v-model="draft.games" />
          <div class="settings-sub">
            <div v-if="candidate" class="flex flex-wrap items-center gap-2" role="group" :aria-label="$t('gaming.addLast')">
              <span class="min-w-0 flex-1 truncate text-sm">{{ $t('gaming.addLastConfirm', { name: candidate }) }}</span>
              <button type="button" class="btn btn-primary btn-sm" data-testid="add-confirm" @click="addCandidate">{{ $t('gaming.addConfirm') }}</button>
              <button type="button" class="btn btn-secondary btn-sm" data-testid="add-cancel" @click="candidate = null">{{ $t('gaming.addCancel') }}</button>
            </div>
            <button v-else type="button" class="btn btn-secondary self-start" data-testid="add-last" @click="askCandidate">{{ $t('gaming.addLast') }}</button>
            <p v-if="addNote" role="status" class="settings-hint">{{ addNote === 'none' ? $t('gaming.addLastNone') : $t('gaming.addFailed') }}</p>
          </div>
        </div>
      </section>
      <div class="flex justify-end"><button type="submit" class="btn btn-primary">{{ $t('common.save') }}</button></div>
    </form>
    <p v-if="error" role="alert" class="text-sm text-mnema-danger mt-3">{{ $t('gaming.error') }}</p>
  </section>
</template>
<style scoped>
.gaming-surface {margin:8px;padding:14px;border-radius:12px;color:var(--color-mnema-text,#eee);background:rgba(18,20,28,var(--sidepeek-opacity,.55));border:none;font-size:13px}
.interactive {font-size:14px;padding:20px;background:rgba(18,20,28,.92);max-height:calc(100vh - 16px);overflow-y:auto;overflow-x:hidden}
.gaming-form {display:flex;flex-direction:column;gap:1.75rem;margin:1.25rem auto 0;max-width:36rem;min-width:0}
header button, .controls button {padding:8px;border-radius:7px;background:rgba(255,255,255,.08)}
.controls button[aria-pressed="true"] {background:rgba(239,68,68,.25)}
header button svg, .controls button svg {width:18px;height:18px}
.speaker-dot {width:7px;height:7px;border-radius:50%;background:#71717a;flex-shrink:0}
.speaker-dot.live {background:#5ee6a8;box-shadow:0 0 8px #5ee6a8}
.speaking {background:rgba(94,230,168,.1)}
</style>
