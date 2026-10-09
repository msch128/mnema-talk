<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { t } from './i18n'
import App from './App.vue'
import { useChatStore } from './stores/chat'
import { useVoiceStore } from './stores/voice'
import { useAuthStore } from './stores/auth'
import { initializeNativeContext, connectNative, disconnectNative } from './lib/nativeTransport'
import { navigate, popRedirectRoute } from './lib/router'
import { isWebDesktopSelector } from './lib/desktopRuntime'
import { invoke } from '@tauri-apps/api/core'

const address = ref('')
const addressInput = ref<HTMLInputElement | null>(null)
const ready = ref(false)
const busy = ref(false)
const connected = ref(false)
const error = ref('')
const origin = ref('')
let attempt = 0
let alive = true
onBeforeUnmount(() => { alive = false; ++attempt })
onMounted(async () => {
  try { if (!isWebDesktopSelector()) await initializeNativeContext(); if (alive) ready.value = true }
  catch { if (alive) error.value = t('nativeDesktop.contextError') }
})
async function connect() {
  if (!ready.value || busy.value) return
  const submittedAddress = addressInput.value?.value ?? address.value
  address.value = submittedAddress
  const current = ++attempt; busy.value = true; error.value = ''
  try {
    if (isWebDesktopSelector()) {
      await invoke('desktop_open_instance', { address: submittedAddress })
      return
    }
    const reply = await connectNative(submittedAddress)
    if (current !== attempt) return
    const body = reply.body
    if (reply.status !== 200 || typeof body !== 'object' || body === null || !('origin' in body) || typeof body.origin !== 'string' || !('compatibility' in body) || !['supported', 'deprecated'].includes(String(body.compatibility)) || !('transport_preview' in body) || body.transport_preview !== true || !('content_authorization' in body) || body.content_authorization !== 'unavailable') throw new Error('Unsupported server')
    const url = new URL(body.origin)
    if (url.protocol !== 'https:' || url.origin !== body.origin || url.username || url.password) throw new Error('Invalid origin')
    origin.value = body.origin; connected.value = true
  } catch (cause) {
    if (current === attempt) error.value = isWebDesktopSelector() && typeof cause === 'string'
      ? cause.slice(0, 300) : t('nativeDesktop.connectError')
  } finally { if (current === attempt) busy.value = false }
}
async function changeServer() {
  ++attempt; connected.value = false; busy.value = true; error.value = ''
  useChatStore().resetCommunityState()
  useVoiceStore().disconnect()
  useAuthStore().resetLocalSession()
  popRedirectRoute()
  navigate('/', { replace: true })
  try { await disconnectNative() }
  catch { error.value = t('nativeDesktop.disconnectError') }
  finally { busy.value = false; origin.value = '' }
}
</script>

<template>
  <template v-if="connected">
    <App />
    <aside class="native-preview-tag">
      <span :title="origin">{{ origin }} · {{ t('nativeDesktop.dev') }}</span>
      <button type="button" @click="changeServer">{{ t('nativeDesktop.changeServer') }}</button>
    </aside>
  </template>
  <main v-else class="native-connect-page">
    <section class="native-connect-panel" aria-labelledby="native-connect-title">
      <div class="native-connect-heading"><span class="native-connect-mark" aria-hidden="true">M</span><h1 id="native-connect-title">Mnema Talk</h1></div>
      <p>{{ t('nativeDesktop.connectCommunity') }}</p>
      <form @submit.prevent="connect">
        <label for="native-server-url">{{ t('nativeDesktop.address') }}</label>
        <input id="native-server-url" ref="addressInput" v-model="address" type="text" inputmode="url" :placeholder="t('nativeDesktop.placeholder')" autocomplete="off" autocapitalize="off" spellcheck="false" :disabled="!ready || busy" required />
        <button type="submit" :disabled="!ready || busy">{{ busy ? t('nativeDesktop.checking') : t('nativeDesktop.connect') }}</button>
      </form>
      <p v-if="error" class="native-connect-error" role="alert">{{ error }}</p>
      <p class="native-connect-note">{{ t('nativeDesktop.dev') }}</p>
    </section>
  </main>
</template>

<style scoped>
.native-connect-page { min-height: 100vh; display: grid; place-items: center; padding: 24px; background: #0f1110; color: #e6eae8; font-size: 13px; }
.native-connect-panel { width: min(100%, 340px); }
.native-connect-heading { display: flex; align-items: center; gap: 10px; margin-bottom: 18px; }
.native-connect-mark { width: 28px; height: 28px; display: grid; place-items: center; color: #2da771; background: #1f3a2e; border-radius: 7px; font-weight: 600; }
h1 { font-size: 18px; font-weight: 600; margin: 0; }
p { color: #9ba69f; margin: 0 0 16px; line-height: 1.5; }
label { display: block; margin-bottom: 6px; color: #cbd3ce; font-size: 12px; }
input { width: 100%; padding: 9px 11px; border: 1px solid #2c322f; border-radius: 6px; background: #171918; color: #e6eae8; outline: none; }
input:focus { border-color: #2da771; }
form button { width: 100%; margin-top: 12px; padding: 9px; border-radius: 6px; background: #257c56; color: #e6eae8; }
button:disabled { opacity: .5; cursor: default; }
.native-connect-note { margin-top: 16px; font-size: 11px; color: #748279; }
.native-connect-error { margin-top: 12px; color: #e2b3a6; }
.native-preview-tag { position: fixed; bottom: 4px; left: 50%; transform: translateX(-50%); max-width: calc(100vw - 32px); display: flex; align-items: center; gap: 12px; padding: 4px 8px; border: 1px solid #2c322f; border-radius: 5px; background: #171918; color: #9ba69f; font-size: 10px; z-index: 100; }
.native-preview-tag span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.native-preview-tag button { flex-shrink: 0; color: #2da771; }
</style>
