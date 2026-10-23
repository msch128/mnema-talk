<script setup lang="ts">
// The second step of signing in to an account with two-factor sign-in: the
// password was right, now the six-digit code of the authenticator app or one
// of the recovery codes. The challenge lives five minutes on the server.
import { ref } from 'vue'
import { Loader2, ShieldCheck } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { t } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'

const props = defineProps<{ challenge: string }>()
const emit = defineEmits<{ back: [] }>()

const authStore = useAuthStore()
const toasts = useToastStore()
const code = ref('')
const busy = ref(false)
const error = ref('')

async function submit() {
  error.value = ''
  busy.value = true
  try {
    const outcome = await authStore.secondStep(props.challenge, code.value)
    if (outcome.kind === 'user' && outcome.notice) {
      toasts.push({ type: 'error', text: t('security.twoFactor.notice', { count: outcome.notice.failed_attempts }), detail: t('security.twoFactor.noticeHint') })
    }
  } catch (err) {
    error.value = caughtErrorMessage(err, t('login.failed'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="space-y-3.5" data-testid="second-factor" @submit.prevent="submit">
    <p class="text-sm text-mnema-muted">{{ $t('security.twoFactor.loginIntro') }}</p>
    <div>
      <label for="login-code" class="mb-1 block font-mono text-xs font-medium uppercase tracking-wider text-mnema-muted">{{ $t('security.twoFactor.code') }}</label>
      <input
        id="login-code"
        v-model="code"
        type="text"
        required
        autocomplete="one-time-code"
        autocapitalize="none"
        spellcheck="false"
        maxlength="32"
        class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 font-mono text-sm tracking-widest text-mnema-text outline-hidden transition focus:border-mnema-accent"
        data-testid="second-factor-code"
      />
    </div>
    <div v-if="error" role="alert" class="rounded-md border border-mnema-danger/30 bg-mnema-danger/10 p-2.5 text-sm text-mnema-danger">{{ error }}</div>
    <button type="submit" :disabled="busy" class="btn btn-primary flex w-full items-center justify-center gap-2">
      <Loader2 v-if="busy" class="h-3.5 w-3.5 animate-spin" /><ShieldCheck v-else class="h-3.5 w-3.5" />
      <span>{{ $t('security.twoFactor.verify') }}</span>
    </button>
    <button type="button" class="w-full text-sm text-mnema-muted transition hover:text-mnema-text" data-testid="second-factor-back" @click="emit('back')">{{ $t('security.twoFactor.back') }}</button>
  </form>
</template>
