<script setup lang="ts">
// "Forgot password?" on the sign-in screen. The server answers the same way
// whether or not the address belongs to an account, and so does this dialog.
import { ref } from 'vue'
import { Loader2, Mail } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const emit = defineEmits<{ close: [] }>()

const email = ref('')
const busy = ref(false)
const sent = ref(false)
const error = ref('')

async function submit() {
  error.value = ''
  busy.value = true
  try {
    await api('/api/auth/password-reset/request', { method: 'POST', json: { email: email.value.trim() } })
    sent.value = true
  } catch (err) {
    error.value = caughtErrorMessage(err, t('errors.unknown'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseDialog :title="$t('recovery.forgotTitle')" panel-class="max-w-sm" initial-focus="input" @close="emit('close')">
    <div class="space-y-4 p-5" data-testid="forgot-password">
      <p v-if="sent" role="status" class="text-sm text-mnema-text" data-testid="forgot-sent">{{ $t('recovery.forgotSent') }}</p>
      <form v-else class="space-y-3.5" @submit.prevent="submit">
        <p class="text-sm text-mnema-muted">{{ $t('recovery.forgotIntro') }}</p>
        <div>
          <label for="forgot-email" class="mb-1 block font-mono text-xs font-medium uppercase tracking-wider text-mnema-muted">{{ $t('security.email.address') }}</label>
          <input id="forgot-email" v-model="email" type="email" required autocomplete="email" maxlength="254" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden transition focus:border-mnema-accent" />
        </div>
        <div v-if="error" role="alert" class="rounded-md border border-mnema-danger/30 bg-mnema-danger/10 p-2.5 text-sm text-mnema-danger">{{ error }}</div>
        <button type="submit" :disabled="busy" class="btn btn-primary flex w-full items-center justify-center gap-2">
          <Loader2 v-if="busy" class="h-3.5 w-3.5 animate-spin" /><Mail v-else class="h-3.5 w-3.5" />
          <span>{{ $t('recovery.forgotSubmit') }}</span>
        </button>
      </form>
      <button type="button" class="w-full text-sm text-mnema-muted transition hover:text-mnema-text" @click="emit('close')">{{ $t('common.close') }}</button>
    </div>
  </BaseDialog>
</template>
