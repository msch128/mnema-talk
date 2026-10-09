<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, toRaw } from 'vue'
import { t } from '../i18n'
import { invoke } from '@tauri-apps/api/core'
import { Mic, MicOff, Headphones, HeadphoneOff, Monitor, X, Settings } from '@lucide/vue'
import { chordFromKey, chordText, type GamingSettings, type GamingSnapshot } from '../lib/desktopGaming'
const interactive = Reflect.get(window, '__MNEMA_GAMING_SURFACE__') === 'gaming-overlay'
const snapshot = ref<GamingSnapshot | null>(null)
const labels = computed(() => ({ overlay: t('gaming.overlay'), mute: t('gaming.mute'), deafen: t('gaming.deafen'), ptt: t('gaming.ptt') }))
const editing = ref(false)
const draft = ref<GamingSettings | null>(null)
const error = ref(false)
const members = computed(() => interactive ? snapshot.value?.voice.members ?? [] : (snapshot.value?.voice.members ?? []).slice(0, 12))
let timer: ReturnType<typeof setInterval> | undefined
let pending = false; let alive = true
async function refresh() {
  if (!alive || pending) return
  pending = true
  try { const next = await invoke<GamingSnapshot>('desktop_gaming_snapshot'); if (alive) snapshot.value = next }
  catch { snapshot.value = null } finally { pending = false }
}
async function control(action: string) {
  try { await invoke('desktop_gaming_control', { action }); error.value = false } catch { error.value = true }
}
function settings() { draft.value = snapshot.value ? structuredClone(toRaw(snapshot.value.settings)) : null; editing.value = !editing.value; error.value = false }
function bind(key: 'overlay' | 'mute' | 'deafen' | 'ptt', event: KeyboardEvent) {
  event.preventDefault(); event.stopPropagation()
  const chord = chordFromKey(event)
  if (chord && draft.value) draft.value[key] = chord
}
async function save() {
  if (!draft.value) return
  try { await invoke('desktop_gaming_settings', { settings: draft.value }); editing.value = false; error.value = false }
  catch { error.value = true }
}
onMounted(() => {
  document.documentElement.style.background = 'transparent'; document.body.style.background = 'transparent'
  void refresh(); timer = setInterval(() => { void refresh() }, 200)
})
onBeforeUnmount(() => { alive = false; clearInterval(timer) })
</script>
<template>
  <section v-if="snapshot?.active" class="gaming-surface" :class="{ interactive }" :aria-label="$t('gaming.title')">
    <header class="flex items-center justify-between gap-2 mb-3">
      <strong>{{ $t('gaming.title') }}</strong>
      <span class="text-xs text-mnema-muted">{{ snapshot.voice.members.length }}</span>
      <template v-if="interactive">
        <button :aria-label="$t('gaming.settings')" @click="settings"><Settings class="w-4 h-4" /></button>
        <button :aria-label="$t('common.close')" @click="control('close')"><X class="w-4 h-4" /></button>
      </template>
    </header>
    <div v-if="interactive" class="flex gap-2 mb-3">
      <button :aria-label="snapshot.voice.muted ? $t('voice.unmute') : $t('voice.mute')" :aria-pressed="snapshot.voice.muted" @click="control('mute')"><MicOff v-if="snapshot.voice.muted" /><Mic v-else /></button>
      <button :aria-label="snapshot.voice.deafened ? $t('voice.undeafen') : $t('voice.deafen')" :aria-pressed="snapshot.voice.deafened" @click="control('deafen')"><HeadphoneOff v-if="snapshot.voice.deafened" /><Headphones v-else /></button>
      <button :aria-label="snapshot.voice.sharing ? $t('voice.stopShare') : $t('voice.share')" :aria-pressed="snapshot.voice.sharing" @click="control('stream')"><Monitor /></button>
      <button :aria-pressed="snapshot.voice.ptt_mode" @click="control('ptt-mode')">{{ $t('gaming.ptt') }}</button>
    </div>
    <ul class="space-y-2 overflow-y-auto max-h-72">
      <li v-for="member in members" :key="member.id" class="flex items-center gap-2 rounded-md px-2 py-1" :class="{ speaking: member.speaking }">
        <span class="speaker-dot" :class="{ live: member.speaking }"></span>
        <span class="truncate flex-1">{{ member.name }}</span>
        <MicOff v-if="member.muted" class="w-4 h-4 text-mnema-danger" :aria-label="$t('voice.mute')" />
        <Monitor v-if="member.sharing" class="w-4 h-4 text-mnema-accent" :aria-label="$t('voice.share')" />
      </li>
    </ul>
    <p v-if="!interactive" class="mt-3 text-xs text-mnema-muted">{{ chordText(snapshot.settings.overlay) }}</p>
    <form v-if="interactive && editing && draft" class="mt-4 space-y-2" @submit.prevent="save">
      <label v-for="key in (['overlay', 'mute', 'deafen', 'ptt'] as const)" :key="key" class="flex items-center justify-between gap-2">
        <span>{{ labels[key] }}</span>
        <input :aria-label="labels[key]" :value="chordText(draft[key])" readonly class="w-36 rounded bg-mnema-canvas p-2" @keydown="bind(key, $event)" />
      </label>
      <label class="block">{{ $t('gaming.games') }}<textarea :value="draft.games.join('\n')" class="block w-full rounded bg-mnema-canvas p-2" @input="draft.games = ($event.target as HTMLTextAreaElement).value.split('\n').map(s => s.trim()).filter(Boolean)" /></label>
      <button type="submit">{{ $t('common.save') }}</button>
    </form>
    <p v-if="error" role="alert" class="text-sm text-mnema-danger mt-3">{{ $t('gaming.error') }}</p>
  </section>
</template>
<style scoped>
.gaming-surface {margin:8px;padding:14px;border-radius:12px;color:var(--color-mnema-text,#eee);background:rgba(18,20,28,.92);border:1px solid rgba(255,255,255,.14);font-size:13px}
.interactive {font-size:14px;padding:20px}
button {padding:8px;border-radius:7px;background:rgba(255,255,255,.08)}
button[aria-pressed="true"] {background:rgba(239,68,68,.25)}
button svg {width:18px;height:18px}
.speaker-dot {width:7px;height:7px;border-radius:50%;background:#71717a;flex-shrink:0}
.speaker-dot.live {background:#5ee6a8;box-shadow:0 0 8px #5ee6a8}
.speaking {background:rgba(94,230,168,.1)}
</style>
