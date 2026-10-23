<script setup lang="ts">
// The ten recovery codes, shown once after turning two-factor sign-in on or
// after making new ones. The server keeps only hashes, so this is the only
// time they can be read.
import { ref } from 'vue'
import { Copy, Download } from '@lucide/vue'
import { t } from '../i18n'
import { useToastStore } from '../stores/toast'

const props = defineProps<{ codes: string[] }>()
const emit = defineEmits<{ done: [] }>()

const toasts = useToastStore()
const saved = ref(false)

async function copy() {
  try {
    await navigator.clipboard.writeText(props.codes.join('\n'))
    toasts.success(t('security.twoFactor.copied'))
  } catch (err) {
    void err
    toasts.error(t('admin.system.copyFailed'))
  }
}

function download() {
  const text = `${t('security.twoFactor.fileHeader')}\n\n${props.codes.join('\n')}\n`
  const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }))
  const a = document.createElement('a')
  a.href = url
  a.download = 'mnema-talk-recovery-codes.txt'
  a.click()
  URL.revokeObjectURL(url)
}
</script>

<template>
  <div class="space-y-3" data-testid="recovery-codes">
    <p class="text-sm text-mnema-muted">{{ $t('security.twoFactor.codesIntro') }}</p>
    <ul class="grid grid-cols-2 gap-1.5 rounded-lg border border-mnema-hairline bg-mnema-canvas p-3 font-mono text-sm text-mnema-text" data-testid="recovery-code-list">
      <li v-for="c in codes" :key="c">{{ c }}</li>
    </ul>
    <div class="flex flex-wrap gap-2 text-xs">
      <button type="button" class="btn btn-secondary btn-sm flex items-center gap-1" data-testid="recovery-copy" @click="copy"><Copy class="h-3.5 w-3.5" />{{ $t('admin.system.copy') }}</button>
      <button type="button" class="btn btn-secondary btn-sm flex items-center gap-1" data-testid="recovery-download" @click="download"><Download class="h-3.5 w-3.5" />{{ $t('security.twoFactor.download') }}</button>
    </div>
    <label class="flex items-start gap-2 text-sm text-mnema-text">
      <input v-model="saved" type="checkbox" class="mt-0.5" data-testid="recovery-saved" />
      <span>{{ $t('security.twoFactor.saved') }}</span>
    </label>
    <button type="button" :disabled="!saved" class="btn btn-primary btn-sm" data-testid="recovery-done" @click="emit('done')">{{ $t('security.twoFactor.done') }}</button>
  </div>
</template>
