<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { captureNativeChatPresentation } from '../lib/nativeTransport'
import { validNativeChatBody, type NativeChatDisplay } from '../lib/nativeChat'
import { useChatStore } from '../stores/chat'
import { t } from '../i18n'
const props = defineProps<{ channelId: string; accountId: string; channelName: string; reload: number }>()
const members = useChatStore()
const messages = ref<NativeChatDisplay[]>([])
const input = ref('')
const busy = ref(false)
const error = ref('')
const pending = ref<{ event: string; body: string } | null>(null)
const disabled = computed(() => busy.value || (!pending.value && input.value.length === 0))
let presentation: ReturnType<typeof captureNativeChatPresentation> | null = null
let generation = 0
let timer: ReturnType<typeof setTimeout> | undefined
function retire() { generation++; if (timer !== undefined) clearTimeout(timer); timer = undefined; presentation?.dispose(); presentation = null; messages.value = []; pending.value = null; input.value = ''; busy.value = false; error.value = '' }
function merge(rows: NativeChatDisplay[]) {
  const existing = new Map(messages.value.map(message => [message.id, message]))
  for (const row of rows) existing.set(row.id, row)
  messages.value = [...existing.values()].sort((a, b) => a.number - b.number).slice(-200)
}
function schedule() { if (timer !== undefined) clearTimeout(timer); timer = setTimeout(() => { void receive() }, 1500) }
async function receive() {
  if (!presentation || busy.value || pending.value) return
  const current = generation; const port = presentation; busy.value = true; error.value = ''
  try { const rows = await port.receive(); if (current !== generation) return; merge(rows); schedule() }
  catch { if (current === generation) error.value = t('nativeChat.loadError') }
  finally { if (current === generation) busy.value = false }
}
async function send() {
  if (!presentation || disabled.value) return
  if (!pending.value && !validNativeChatBody(input.value)) { error.value = t('nativeChat.inputInvalid'); return }
  if (timer !== undefined) clearTimeout(timer)
  const current = generation; const port = presentation
  const outgoing = pending.value ?? { event: crypto.randomUUID(), body: input.value }
  pending.value = outgoing; busy.value = true; error.value = ''
  try {
    const rows = await port.publish(outgoing.body, outgoing.event)
    if (current !== generation) return
    merge(rows); pending.value = null; input.value = ''; schedule()
  } catch { if (current === generation) error.value = t('nativeChat.sendUnconfirmed') }
  finally { if (current === generation) busy.value = false }
}
watch(() => [props.channelId, props.accountId] as const, () => {
  retire()
  try { presentation = captureNativeChatPresentation(props.channelId, props.accountId); messages.value = presentation.snapshot(); void receive() }
  catch { error.value = t('nativeChat.unavailable') }
}, { immediate: true })
watch(() => props.reload, () => { void receive() })
onBeforeUnmount(retire)
function name(account: string) { const member = members.members.find(member => member.id === account); return member?.display_name || member?.username || account }
</script>
<template>
  <section class="native-chat-area" :aria-label="channelName">
    <header><span># {{ channelName }}</span><button type="button" :disabled="busy || !!pending" @click="receive">{{ t('nativeChat.refresh') }}</button></header>
    <div class="native-chat-messages" role="log" aria-live="polite">
      <article v-for="message in messages" :key="message.id"><strong>{{ name(message.account_id) }}</strong><p>{{ message.body }}</p></article>
    </div>
    <p v-if="error" class="native-chat-error" role="alert">{{ error }}</p>
    <form @submit.prevent="send"><textarea v-model="input" :disabled="busy || !!pending" :aria-label="t('nativeChat.messageLabel', { channel: channelName })" rows="2" /><button type="submit" :disabled="disabled">{{ t(busy ? 'nativeChat.sending' : pending ? 'nativeChat.retry' : 'nativeChat.send') }}</button></form>
  </section>
</template>
<style scoped>
.native-chat-area { display: flex; flex-direction: column; min-height: 0; color: #cbd3ce; font-size: 12px; }
header { display: flex; align-items: center; justify-content: space-between; padding: 10px 16px; border-bottom: 1px solid #2c322f; }
header span { font-weight: 600; }
button { color: #2da771; padding: 5px 8px; font-size: 11px; }
button:disabled { opacity: .5; }
.native-chat-messages { flex: 1; overflow: auto; padding: 12px 16px; }
article { margin-bottom: 12px; }
strong { font-size: 11px; color: #9ba69f; }
article p { margin-top: 3px; white-space: pre-wrap; overflow-wrap: anywhere; }
form { display: flex; gap: 8px; padding: 10px 16px; }
textarea { flex: 1; min-width: 0; resize: vertical; padding: 8px; border: 1px solid #2c322f; border-radius: 5px; background: #171918; color: #e6eae8; font-size: 12px; }
.native-chat-error { padding: 0 16px; color: #e2b3a6; font-size: 11px; }
</style>
