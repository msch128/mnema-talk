<script setup lang="ts">
import { ref, onBeforeUnmount, watch } from 'vue'
import BaseDialog from './BaseDialog.vue'
import { t } from '../i18n'
import { decodeTrustPreview, decodeTrustStatus, trustOperationId } from '../lib/nativeTrust'
import type { NativeTrustPresentationPort, TrustPreview, TrustStatus } from '../lib/nativeTrust'
const props = defineProps<{ channelId: string; port: NativeTrustPresentationPort }>()
const emit = defineEmits<{ close: []; status: [state: TrustStatus['state']] }>()
const preview = ref<TrustPreview | null>(null)
const status = ref<TrustStatus['state'] | null>(null)
const busy = ref(false)
const error = ref('')
let incarnation = 0
let alive = true
let unsubscribe: (() => void) | null = null
let activePort = props.port
function retire() {
  incarnation++; unsubscribe?.(); unsubscribe = null
  const previous = preview.value; preview.value = null; status.value = null; busy.value = false
  if (previous) void activePort.cancel(previous.operation_id).catch(() => {})
}
function listen() {
  const owner = incarnation
  unsubscribe = activePort.subscribe(value => {
    if (!alive || owner !== incarnation || !preview.value) return
    try {
      const update = decodeTrustStatus(value)
      if (update.operation_id !== preview.value.operation_id) return
      // A pending callback cannot replace a terminal UI status.
      if (status.value && status.value !== 'pending') return
      status.value = update.state
      emit('status', update.state)
      if (update.state === 'cancelled' || update.state === 'expired' || update.state === 'failed') preview.value = null
    } catch { error.value = t('nativeTrust.statusError') }
  })
}
listen()
watch(() => [props.channelId, props.port] as const, () => { retire(); activePort = props.port; error.value = ''; listen() })
onBeforeUnmount(() => { alive = false; retire() })
async function begin() {
  if (busy.value || preview.value || !trustOperationId(props.channelId)) return
  const owner = incarnation; const currentPort = activePort; const channelId = props.channelId
  busy.value = true; error.value = ''
  try {
    const value = decodeTrustPreview(await currentPort.beginFirstRoot(channelId))
    if (!alive || owner !== incarnation) { void currentPort.cancel(value.operation_id).catch(() => {}); return }
    if (value.operation_kind !== 'first_root' || value.scope.channel_id !== channelId) {
      void currentPort.cancel(value.operation_id).catch(() => {})
      throw new Error('Wrong native trust operation')
    }
    preview.value = value; status.value = 'pending'
  } catch { if (alive && owner === incarnation) error.value = t('nativeTrust.beginError') }
  finally { if (alive && owner === incarnation) busy.value = false }
}
async function requestConfirmation() {
  const current = preview.value
  if (!current || busy.value || status.value !== 'pending') return
  const owner = incarnation; busy.value = true; error.value = ''
  try {
    const value = decodeTrustStatus(await activePort.requestConfirmation(current.operation_id))
    if (!alive || owner !== incarnation || preview.value?.operation_id !== current.operation_id) return
    if (value.operation_id !== current.operation_id || !['pending', 'cancelled'].includes(value.state)) throw new Error('Invalid native confirmation response')
    if (status.value === 'pending') { status.value = value.state; if (value.state === 'cancelled') preview.value = null }
  } catch { if (alive && owner === incarnation) error.value = t('nativeTrust.confirmError') }
  finally { if (alive && owner === incarnation) busy.value = false }
}
function close() { retire(); emit('close') }
</script>
<template>
  <BaseDialog :title="t('nativeTrust.title')" panel-class="max-w-sm" @close="close">
    <div class="trust-content">
      <p>{{ t('nativeTrust.description') }}</p>
      <template v-if="preview">
        <dl>
          <dt>{{ t('nativeTrust.community') }}</dt><dd>{{ preview.scope.community_id }}</dd>
          <dt>{{ t('nativeTrust.server') }}</dt><dd>{{ preview.scope.origin }}</dd>
          <dt>{{ t('nativeTrust.channel') }}</dt><dd>{{ preview.scope.channel_id }}</dd>
          <dt>{{ t('nativeTrust.account') }}</dt><dd>{{ preview.scope.account_id }}</dd>
          <dt>{{ t('nativeTrust.device') }}</dt><dd>{{ preview.scope.device_id }}</dd>
          <dt>{{ t('nativeTrust.fingerprint') }}</dt><dd class="fingerprint">{{ preview.root_fingerprint_hex }}</dd>
        </dl>
        <p v-if="status === 'root_saved'" role="status">{{ t('nativeTrust.saved') }}</p>
        <button v-else type="button" :disabled="busy || status !== 'pending'" @click="requestConfirmation">{{ busy ? t('nativeTrust.opening') : t('nativeTrust.confirm') }}</button>
      </template>
      <button v-else type="button" :disabled="busy || !trustOperationId(channelId)" @click="begin">{{ busy ? t('nativeTrust.preparing') : t('nativeTrust.prepare') }}</button>
      <p v-if="status === 'expired'" role="status">{{ t('nativeTrust.expired') }}</p>
      <p v-if="status === 'cancelled'" role="status">{{ t('nativeTrust.cancelled') }}</p>
      <p v-if="status === 'failed'" role="status">{{ t('nativeTrust.failed') }}</p>
      <p v-if="error" class="trust-error" role="alert">{{ error }}</p>
      <button type="button" class="trust-close" @click="close">{{ t('common.close') }}</button>
    </div>
  </BaseDialog>
</template>
<style scoped>
.trust-content { padding: 16px 20px; font-size: 12px; color: #cbd3ce; }
p { margin: 0 0 12px; line-height: 1.5; color: #9ba69f; }
dl { margin-bottom: 16px; }
dt { margin-top: 9px; color: #748279; font-size: 10px; }
dd { margin: 3px 0 0; overflow-wrap: anywhere; }
.fingerprint { font-family: monospace; font-size: 11px; }
button { border-radius: 5px; padding: 7px 10px; color: #e6eae8; background: #257c56; font-size: 12px; }
button:disabled { opacity: .5; }
.trust-close { margin-left: 8px; background: #2c322f; }
.trust-error { margin-top: 12px; color: #e2b3a6; }
</style>
