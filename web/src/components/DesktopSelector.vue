<script setup lang="ts">
import { ref, onBeforeUnmount } from 'vue'
import { t } from '../i18n'
import { platform } from '../lib/platform'
import { localizeHostError } from '../lib/hostError'
import { copyText } from '../lib/copyText'

const address = ref('')
const verified = platform.trustedApp()
const addressInput = ref<HTMLInputElement | null>(null)
const busy = ref(false)
const error = ref('')
const rawError = ref('')
const copied = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | undefined
let attempt = 0
onBeforeUnmount(() => { ++attempt; clearTimeout(copiedTimer) })

function hostFailure(cause: unknown): string {
  return typeof cause === 'string' ? localizeHostError(cause.slice(0, 600)) : t('nativeDesktop.connectError')
}

// The shown sentence plus, when the host sent it differently, the host's own wording as the error code.
async function copyError() {
  const text = rawError.value && rawError.value !== error.value ? `${error.value}\n${rawError.value}` : error.value
  if (!await copyText(text)) return
  copied.value = true
  clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => { copied.value = false }, 2000)
}

async function connect() {
  if (busy.value) return
  const submittedAddress = addressInput.value?.value ?? address.value
  address.value = submittedAddress
  const current = ++attempt
  busy.value = true
  error.value = ''
  rawError.value = ''
  copied.value = false
  try {
    await platform.invoke('desktop_open_instance', { address: submittedAddress })
  } catch (cause) {
    if (current === attempt) {
      error.value = hostFailure(cause)
      rawError.value = typeof cause === 'string' ? cause.slice(0, 600) : ''
    }
  } finally {
    if (current === attempt) busy.value = false
  }
}
</script>

<template>
  <main class="native-connect-page">
    <section class="native-connect-panel" aria-labelledby="native-connect-title">
      <div class="native-connect-heading"><span class="native-connect-mark" aria-hidden="true">M</span><h1 id="native-connect-title">Mnema Talk</h1></div>
      <p>{{ t('nativeDesktop.connectCommunity') }}</p>
      <form @submit.prevent="connect">
        <label for="native-server-url">{{ t('nativeDesktop.address') }}</label>
        <input id="native-server-url" ref="addressInput" v-model="address" type="text" inputmode="url" :placeholder="t('nativeDesktop.placeholder')" autocomplete="off" autocapitalize="off" spellcheck="false" :disabled="busy" required />
        <button type="submit" :disabled="busy">{{ busy ? t('nativeDesktop.checking') : t('nativeDesktop.connect') }}</button>
      </form>
      <div v-if="error" class="native-connect-error">
        <p role="alert" data-testid="connect-error">{{ error }}</p>
        <button type="button" class="native-connect-copy" data-testid="copy-error" @click="copyError">{{ copied ? t('nativeDesktop.copied') : t('nativeDesktop.copy') }}</button>
      </div>
      <p v-if="verified" class="native-connect-note" data-testid="verified-app">{{ t('trustedApp.verified', { version: verified.version }) }} · {{ t(`trustedApp.${verified.source}`) }}</p>
      <p v-else class="native-connect-note">{{ t('nativeDesktop.dev') }}</p>
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
.native-connect-error p { margin: 0 0 6px; color: inherit; user-select: text; -webkit-user-select: text; cursor: text; overflow-wrap: anywhere; }
.native-connect-copy { padding: 3px 10px; border: 1px solid #2c322f; border-radius: 6px; background: transparent; color: #9ba69f; font-size: 12px; }
.native-connect-copy:hover { color: #e6eae8; border-color: #3a423e; }
</style>
