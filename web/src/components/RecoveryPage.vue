<script setup lang="ts">
// The page behind the links in account e-mails (/verify-email, /reset-password).
// It works signed in or out: the token in the link is the credential. The token
// is read once from the link and the address bar is cleaned right away.
import type { PropType } from 'vue'
import { computed, onMounted, ref } from 'vue'
import { CheckCircle2, KeyRound, Loader2, XCircle } from '@lucide/vue'
import { api, caughtErrorMessage, isApiError } from '../lib/api'
import { MIN_PASSWORD_LENGTH } from '../lib/passwordPolicy'
import { scrubRecoveryUrl, type RecoveryLink } from '../lib/recoveryLink'
import { t } from '../i18n'

const props = defineProps({ link: { type: Object as PropType<RecoveryLink>, required: true } })
const emit = defineEmits<{ done: [] }>()

const token = props.link.token
const kind = props.link.kind
type Phase = 'working' | 'form' | 'success' | 'failed'
const phase = ref<Phase>(token ? (kind === 'verify' ? 'working' : 'form') : 'failed')
const message = ref(token ? '' : t('recovery.linkInvalid'))
const password = ref('')
const repeat = ref('')
const busy = ref(false)

const heading = computed(() => t(kind === 'verify' ? 'recovery.verifyTitle' : 'recovery.resetTitle'))

async function verify() {
  try {
    await api('/api/auth/email/verify', { method: 'POST', json: { token } })
    phase.value = 'success'
    message.value = t('recovery.verified')
  } catch (err) {
    phase.value = 'failed'
    message.value = isApiError(err) && err.status === 409 ? t('recovery.addressInUse') : t('recovery.linkInvalid')
  }
}

async function submitReset() {
  message.value = ''
  if ([...password.value].length < MIN_PASSWORD_LENGTH) {
    message.value = t('profile.passwordTooShort', { count: MIN_PASSWORD_LENGTH })
    return
  }
  if (password.value !== repeat.value) {
    message.value = t('profile.passwordMismatch')
    return
  }
  busy.value = true
  try {
    await api('/api/auth/password-reset/confirm', { method: 'POST', json: { token, new_password: password.value } })
    password.value = repeat.value = ''
    phase.value = 'success'
    message.value = t('recovery.resetDone')
  } catch (err) {
    // A rejected password keeps the link usable; only a dead link ends the form.
    message.value = caughtErrorMessage(err, t('errors.unknown'))
    if (message.value === t('errors.detail.linkInvalid')) phase.value = 'failed'
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  // Remove the token from the address bar and history, keep the page path.
  scrubRecoveryUrl(window.location.pathname)
  if (token && kind === 'verify') void verify()
})
</script>

<template>
  <div class="flex flex-1 items-center justify-center p-4" data-testid="recovery-page">
    <div class="flex w-full max-w-sm flex-col space-y-5 rounded-[14px] border border-mnema-border bg-mnema-elevated p-7 shadow-2xl">
      <div class="space-y-2 text-center">
        <div class="mx-auto flex h-9 w-9 items-center justify-center rounded-lg border border-mnema-mint/30 bg-mnema-band text-base font-bold text-mnema-mint shadow-sm">M</div>
        <h1 class="text-lg font-bold tracking-tight text-mnema-text">{{ heading }}</h1>
      </div>

      <div v-if="phase === 'working'" role="status" class="flex items-center justify-center gap-2 text-sm text-mnema-muted">
        <Loader2 class="h-4 w-4 animate-spin" />{{ $t('recovery.verifying') }}
      </div>

      <form v-else-if="phase === 'form'" class="space-y-3.5" @submit.prevent="submitReset">
        <p class="text-sm text-mnema-muted">{{ $t('recovery.resetIntro') }}</p>
        <div>
          <label for="recovery-password" class="mb-1 block font-mono text-xs font-medium uppercase tracking-wider text-mnema-muted">{{ $t('profile.newPassword', { count: MIN_PASSWORD_LENGTH }) }}</label>
          <input id="recovery-password" v-model="password" type="password" required autocomplete="new-password" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-none transition focus:border-mnema-accent" />
        </div>
        <div>
          <label for="recovery-repeat" class="mb-1 block font-mono text-xs font-medium uppercase tracking-wider text-mnema-muted">{{ $t('profile.repeatPassword') }}</label>
          <input id="recovery-repeat" v-model="repeat" type="password" required autocomplete="new-password" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-none transition focus:border-mnema-accent" />
        </div>
        <div v-if="message" role="alert" class="rounded-md border border-mnema-danger/30 bg-mnema-danger/10 p-2.5 text-sm text-mnema-danger">{{ message }}</div>
        <button type="submit" :disabled="busy" class="flex w-full items-center justify-center gap-2 rounded-md bg-mnema-accent py-2 text-sm font-semibold text-mnema-accent-ink shadow-sm transition hover:bg-mnema-accent-hover disabled:opacity-50">
          <Loader2 v-if="busy" class="h-3.5 w-3.5 animate-spin" /><KeyRound v-else class="h-3.5 w-3.5" />
          <span>{{ $t('recovery.resetSubmit') }}</span>
        </button>
      </form>

      <div v-else class="space-y-4 text-center">
        <component :is="phase === 'success' ? CheckCircle2 : XCircle" :class="['mx-auto h-8 w-8', phase === 'success' ? 'text-mnema-accent' : 'text-mnema-danger']" aria-hidden="true" />
        <p role="status" class="text-sm text-mnema-text" data-testid="recovery-message">{{ message }}</p>
        <button type="button" class="w-full rounded-md bg-mnema-accent py-2 text-sm font-semibold text-mnema-accent-ink shadow-sm transition hover:bg-mnema-accent-hover" @click="emit('done')">{{ $t('recovery.continue') }}</button>
      </div>
    </div>
  </div>
</template>
