<script setup lang="ts">
// Two-factor sign-in (authenticator app, TOTP) of the signed-in member: turn
// it on with a QR code and a first code, keep ten recovery codes, make new
// ones or turn it off again. Every change needs the password and, once it is
// on, a current code.
import { computed, ref } from 'vue'
import { Loader2, ShieldCheck, ShieldOff } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { disableTwoFactor, enableTwoFactor, newRecoveryCodes, setupTwoFactor } from '../lib/accountApi'
import type { AuthTwoFactorSetup } from '../types/rest'
import { t } from '../i18n'
import { useAccountStore } from '../stores/account'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import QrCode from './QrCode.vue'
import RecoveryCodesPanel from './RecoveryCodesPanel.vue'
import FormField from './FormField.vue'
import PasswordInput from './PasswordInput.vue'

const account = useAccountStore()
const authStore = useAuthStore()
const toasts = useToastStore()

type Step = 'idle' | 'password' | 'scan' | 'codes' | 'proof-disable' | 'proof-codes'
const step = ref<Step>('idle')
const password = ref('')
const code = ref('')
const setup = ref<AuthTwoFactorSetup | null>(null)
const codes = ref<string[]>([])
const busy = ref(false)
const error = ref('')

const enabled = computed(() => !!account.info?.two_factor_enabled)
const remaining = computed(() => account.info?.recovery_codes_remaining ?? 0)
// The Owner holds every permission, so their account gets the nudge.
const recommend = computed(() => authStore.isAdmin && !enabled.value && step.value === 'idle')

function go(next: Step) {
  password.value = ''
  code.value = ''
  error.value = ''
  step.value = next
}

async function run(action: () => Promise<void>) {
  error.value = ''
  busy.value = true
  try {
    await action()
  } catch (err) {
    error.value = caughtErrorMessage(err, t('errors.unknown'))
  } finally {
    busy.value = false
  }
}

const start = () => run(async () => {
  setup.value = await setupTwoFactor(password.value)
  go('scan')
})

const confirm = () => run(async () => {
  codes.value = await enableTwoFactor(code.value)
  // The codes are shown once: reveal them before the refresh, which may fail.
  setup.value = null
  go('codes')
  await account.load().catch(() => undefined)
})

const turnOff = () => run(async () => {
  await disableTwoFactor(password.value, code.value)
  await account.load()
  toasts.success(t('security.twoFactor.off'))
  go('idle')
})

const renew = () => run(async () => {
  codes.value = await newRecoveryCodes(password.value, code.value)
  go('codes')
  await account.load().catch(() => undefined)
})

function finish() {
  codes.value = []
  go('idle')
}
</script>

<template>
  <section class="settings-card" data-testid="account-two-factor" aria-labelledby="security-2fa-title">
    <div>
      <h3 id="security-2fa-title" class="settings-title">{{ $t('security.twoFactor.title') }}</h3>
      <p class="settings-help">{{ $t('security.twoFactor.intro') }}</p>
    </div>

    <div v-if="recommend" role="note" class="rounded-lg border border-mnema-warning/35 bg-mnema-warning/10 p-2.5 text-sm text-mnema-text" data-testid="two-factor-recommend">{{ $t('security.twoFactor.recommendOwner') }}</div>

    <div v-if="step === 'idle'" class="space-y-3">
      <p class="flex items-center gap-2 text-sm" data-testid="two-factor-state">
        <component :is="enabled ? ShieldCheck : ShieldOff" :class="['h-4 w-4', enabled ? 'text-mnema-accent' : 'text-mnema-tertiary']" aria-hidden="true" />
        <span class="text-mnema-text">{{ $t(enabled ? 'security.twoFactor.on' : 'security.twoFactor.offState') }}</span>
        <span v-if="enabled" class="text-xs text-mnema-tertiary" data-testid="two-factor-remaining">{{ $t('security.twoFactor.remaining', { count: remaining }) }}</span>
      </p>
      <p v-if="enabled && remaining <= 3" role="note" class="text-xs text-mnema-warning" data-testid="two-factor-low">{{ $t('security.twoFactor.low') }}</p>
      <div class="flex flex-wrap gap-2">
        <button v-if="!enabled" type="button" class="btn btn-primary" data-testid="two-factor-start" @click="go('password')">{{ $t('security.twoFactor.turnOn') }}</button>
        <template v-else>
          <button type="button" class="btn btn-secondary" data-testid="two-factor-renew" @click="go('proof-codes')">{{ $t('security.twoFactor.newCodes') }}</button>
          <button type="button" class="btn btn-secondary hover:border-mnema-danger hover:text-mnema-danger" data-testid="two-factor-off" @click="go('proof-disable')">{{ $t('security.twoFactor.turnOff') }}</button>
        </template>
      </div>
    </div>

    <form v-else-if="step === 'password'" class="space-y-3" @submit.prevent="start">
      <FormField v-slot="{ id }" :label="$t('profile.currentPassword')">
        <PasswordInput :id="id" v-model="password" required autocomplete="current-password" data-testid="two-factor-password" />
      </FormField>
      <div v-if="error" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-1.5 text-sm text-mnema-danger">{{ error }}</div>
      <div class="flex justify-end gap-2 pt-1">
        <button type="button" class="btn btn-ghost" @click="go('idle')">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="busy" class="btn btn-primary" data-testid="two-factor-continue">{{ $t('security.twoFactor.continue') }}</button>
      </div>
    </form>

    <form v-else-if="step === 'scan' && setup" class="space-y-3" @submit.prevent="confirm">
      <p class="text-sm text-mnema-muted">{{ $t('security.twoFactor.scan') }}</p>
      <div class="flex justify-center"><QrCode :text="setup.otpauth_uri" :label="$t('security.twoFactor.qrLabel')" /></div>
      <p class="text-xs text-mnema-tertiary">{{ $t('security.twoFactor.manual') }}</p>
      <code class="block select-all break-all rounded-md border border-mnema-hairline bg-mnema-canvas p-2 text-center font-mono text-sm text-mnema-text" data-testid="two-factor-secret">{{ setup.secret }}</code>
      <FormField v-slot="{ id }" :label="$t('security.twoFactor.code')">
        <input :id="id" v-model="code" type="text" required inputmode="numeric" autocomplete="one-time-code" maxlength="8" class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 font-mono text-sm tracking-widest text-mnema-text" data-testid="two-factor-code" />
      </FormField>
      <div v-if="error" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-1.5 text-sm text-mnema-danger">{{ error }}</div>
      <div class="flex justify-end gap-2 pt-1">
        <button type="button" class="btn btn-ghost" @click="go('idle')">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="busy" class="btn btn-primary flex items-center gap-1" data-testid="two-factor-enable"><Loader2 v-if="busy" class="h-3.5 w-3.5 animate-spin" />{{ $t('security.twoFactor.confirm') }}</button>
      </div>
    </form>

    <RecoveryCodesPanel v-else-if="step === 'codes'" :codes="codes" @done="finish" />

    <form v-else class="space-y-3" @submit.prevent="step === 'proof-disable' ? turnOff() : renew()">
      <p class="text-sm text-mnema-muted">{{ $t('security.twoFactor.proof') }}</p>
      <FormField v-slot="{ id }" :label="$t('profile.currentPassword')">
        <PasswordInput :id="id" v-model="password" required autocomplete="current-password" data-testid="two-factor-password" />
      </FormField>
      <FormField v-slot="{ id }" :label="$t('security.twoFactor.code')">
        <input :id="id" v-model="code" type="text" required autocomplete="one-time-code" autocapitalize="none" spellcheck="false" maxlength="32" class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 font-mono text-sm tracking-widest text-mnema-text" data-testid="two-factor-code" />
      </FormField>
      <div v-if="error" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-1.5 text-sm text-mnema-danger">{{ error }}</div>
      <div class="flex justify-end gap-2 pt-1">
        <button type="button" class="btn btn-ghost" @click="go('idle')">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="busy" :class="['btn', step === 'proof-disable' ? 'btn-danger-solid' : 'btn-primary']" data-testid="two-factor-proof-submit"><Loader2 v-if="busy" class="h-3.5 w-3.5 animate-spin" />{{ $t(step === 'proof-disable' ? 'security.twoFactor.turnOff' : 'security.twoFactor.newCodes') }}</button>
      </div>
    </form>
  </section>
</template>
