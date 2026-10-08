<script setup lang="ts">
import { ref, shallowRef } from 'vue'
import NativeTrustSetup from './NativeTrustSetup.vue'
import { captureNativeTrustPresentation } from '../lib/nativeTransport'
import type { NativeTrustPresentationPort } from '../lib/nativeTrust'
import { t } from '../i18n'
defineProps<{ channelId: string }>()
const emit = defineEmits<{ saved: [] }>()
const opened = ref(false)
const error = ref(false)
const port = shallowRef<NativeTrustPresentationPort | null>(null)
function open() {
  if (opened.value) return
  error.value = false
  try { port.value = captureNativeTrustPresentation(); opened.value = true }
  catch { error.value = true }
}
function close() { opened.value = false; port.value = null }
</script>
<template>
  <div class="native-trust-control">
    <button type="button" @click="open">{{ t('nativeTrust.title') }}</button>
    <span v-if="error" role="alert">{{ t('nativeTrust.beginError') }}</span>
    <NativeTrustSetup v-if="opened && port" :channel-id="channelId" :port="port" @close="close" @status="state => { if (state === 'root_saved') emit('saved') }" />
  </div>
</template>
<style scoped>
.native-trust-control { display: flex; align-items: center; gap: 8px; padding: 5px 12px; border-bottom: 1px solid #2c322f; background: #171918; font-size: 11px; }
button { color: #2da771; padding: 2px 0; }
span { color: #e2b3a6; }
</style>
