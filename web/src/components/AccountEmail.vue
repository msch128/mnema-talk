<script setup lang="ts">
// Optional e-mail address of the signed-in member: never required, never shown
// to anyone else. Setting or removing it needs the current password and, when
// two-factor sign-in is on, a current code.
import { computed, ref } from 'vue'
import { BadgeCheck, Loader2, Mail, MailQuestion } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { t } from '../i18n'
import { useAccountStore } from '../stores/account'
import { useToastStore } from '../stores/toast'
import FormField from './FormField.vue'
import PasswordInput from './PasswordInput.vue'

const account = useAccountStore()
const toasts = useToastStore()

type Mode = 'view' | 'edit' | 'remove'
const mode = ref<Mode>('view')
const email = ref('')
const password = ref('')
const code = ref('')
const busy = ref(false)
const error = ref('')

const info = computed(() => account.info)
const hasEmail = computed(() => !!info.value?.email)
const canConfirm = computed(() => !!info.value?.mail_available)
const needsCode = computed(() => !!info.value?.two_factor_enabled)

function start(next: Mode) {
  email.value = next === 'edit' ? info.value?.email ?? '' : ''
  password.value = code.value = ''
  error.value = ''
  mode.value = next
}

async function run(action: () => Promise<string | void>, done: string) {
  error.value = ''
  busy.value = true
  try {
    const outcome = await action()
    mode.value = 'view'
    password.value = code.value = ''
    toasts.success(outcome || done)
  } catch (err) {
    error.value = caughtErrorMessage(err, t('errors.unknown'))
    // The address may be saved even when the confirmation mail failed.
    await account.load().catch(() => undefined)
  } finally {
    busy.value = false
  }
}

// Checked here, in the app's language, instead of by the browser's own bubbles.
const LOOKS_LIKE_EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
function submit() {
  if (mode.value === 'edit' && !LOOKS_LIKE_EMAIL.test(email.value.trim())) { error.value = t('security.email.invalid'); return }
  if (!password.value) { error.value = t('security.email.needPassword'); return }
  if (needsCode.value && !code.value.trim()) { error.value = t('security.email.needCode'); return }
  void (mode.value === 'edit' ? save() : remove())
}

const save = () => run(async () => {
  const mailed = await account.setEmail(email.value.trim(), password.value, code.value.trim())
  if (!mailed) return t('security.email.savedNoMail')
}, t(canConfirm.value ? 'security.email.savedConfirm' : 'security.email.saved'))
const remove = () => run(() => account.removeEmail(password.value, code.value.trim()), t('security.email.removed'))

async function resend() {
  busy.value = true
  try {
    await account.resendConfirmation()
    toasts.success(t('security.email.resent'))
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('errors.unknown')))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="settings-card" data-testid="account-email" :aria-labelledby="'security-email-title'">
    <div>
      <h3 id="security-email-title" class="settings-title">{{ $t('security.email.title') }}</h3>
      <p class="settings-help">{{ $t(canConfirm ? 'security.email.intro' : 'security.email.introNoMail') }}</p>
    </div>

    <div v-if="mode === 'view'" class="space-y-3">
      <div v-if="hasEmail" class="flex items-center gap-3 rounded-lg border border-mnema-hairline bg-mnema-canvas/50 px-4 py-3 text-sm">
        <component :is="info?.email_verified ? BadgeCheck : MailQuestion" :class="['h-4 w-4 shrink-0', info?.email_verified ? 'text-mnema-accent' : 'text-mnema-tertiary']" aria-hidden="true" />
        <span class="min-w-0 flex-1 truncate font-mono text-mnema-text" data-testid="account-email-address">{{ info?.email }}</span>
        <span class="whitespace-nowrap text-xs text-mnema-tertiary" data-testid="account-email-state">{{ $t(info?.email_verified ? 'security.email.confirmed' : (canConfirm ? 'security.email.unconfirmed' : 'security.email.unused')) }}</span>
      </div>
      <p v-else class="text-sm text-mnema-tertiary" data-testid="account-email-none">{{ $t('security.email.none') }}</p>
      <div class="flex flex-wrap items-center gap-2">
        <button type="button" class="btn btn-secondary" data-testid="account-email-edit" @click="start('edit')">{{ $t(hasEmail ? 'security.email.change' : 'security.email.add') }}</button>
        <button v-if="hasEmail && !info?.email_verified && canConfirm" type="button" :disabled="busy" class="btn btn-secondary" data-testid="account-email-resend" @click="resend">{{ $t('security.email.resend') }}</button>
        <button v-if="hasEmail" type="button" class="btn btn-secondary hover:border-mnema-danger hover:text-mnema-danger" data-testid="account-email-remove" @click="start('remove')">{{ $t('security.email.remove') }}</button>
      </div>
    </div>

    <form v-else class="space-y-3" novalidate @submit.prevent="submit">
      <FormField v-if="mode === 'edit'" v-slot="{ id }" :label="$t('security.email.address')">
        <input :id="id" v-model="email" type="email" required maxlength="254" autocomplete="email" class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text" data-testid="account-email-input" />
      </FormField>
      <FormField v-slot="{ id }" :label="$t('profile.currentPassword')">
        <PasswordInput :id="id" v-model="password" required autocomplete="current-password" data-testid="account-email-password" />
      </FormField>
      <FormField v-if="needsCode" v-slot="{ id }" :label="$t('security.twoFactor.code')">
        <input :id="id" v-model="code" type="text" required autocomplete="one-time-code" autocapitalize="none" spellcheck="false" maxlength="32" class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 font-mono text-sm tracking-widest text-mnema-text" data-testid="account-email-code" />
      </FormField>
      <div v-if="error" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-1.5 text-sm text-mnema-danger">{{ error }}</div>
      <div class="flex items-center justify-end gap-2 pt-1">
        <button type="button" class="btn btn-ghost" @click="start('view')">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="busy" :class="['btn', mode === 'remove' ? 'btn-danger-solid' : 'btn-primary']" data-testid="account-email-submit">
          <Loader2 v-if="busy" class="h-3.5 w-3.5 animate-spin" /><Mail v-else class="h-3.5 w-3.5" />
          <span>{{ $t(mode === 'edit' ? 'common.save' : 'security.email.remove') }}</span>
        </button>
      </div>
    </form>
  </section>
</template>
