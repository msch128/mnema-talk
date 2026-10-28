<script setup lang="ts">
import { computed, ref } from 'vue'
import { Edit3, Loader2, Save } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { useAccountStore } from '../stores/account'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import { MIN_PASSWORD_LENGTH } from '../lib/passwordPolicy'
import FormField from './FormField.vue'
import PasswordInput from './PasswordInput.vue'

// Changing one's own password (own profile only).
const authStore = useAuthStore()
const account = useAccountStore()
const toasts = useToastStore()

const isChangingPassword = ref(false)
const currentPassword = ref('')
const code = ref('')
// With two-factor sign-in on, the credentials change only with a current code too.
const needsCode = computed(() => !!account.info?.two_factor_enabled)
const newPassword = ref('')
const newPasswordRepeat = ref('')
const isSavingPassword = ref(false)
const passwordError = ref('')

async function savePassword() {
  passwordError.value = ''
  if ([...newPassword.value].length < MIN_PASSWORD_LENGTH) {
    passwordError.value = t('profile.passwordTooShort', { count: MIN_PASSWORD_LENGTH })
    return
  }
  if (newPassword.value !== newPasswordRepeat.value) {
    passwordError.value = t('profile.passwordMismatch')
    return
  }
  isSavingPassword.value = true
  try {
    await authStore.changePassword(currentPassword.value, newPassword.value, needsCode.value ? code.value.trim() : '')
    currentPassword.value = newPassword.value = newPasswordRepeat.value = code.value = ''
    isChangingPassword.value = false
    toasts.success(t('profile.passwordChanged'))
  } catch (err) {
    passwordError.value = caughtErrorMessage(err, t('profile.saveFailed'))
  } finally {
    isSavingPassword.value = false
  }
}
</script>

<template>
  <section class="settings-card" data-testid="account-password">
    <div class="flex items-center justify-between gap-3">
      <h3 class="settings-title">{{ $t('profile.password') }}</h3>
      <button
        v-if="!isChangingPassword"
        @click="isChangingPassword = true; passwordError = ''"
        class="btn btn-secondary"
      >
        <Edit3 class="w-4 h-4" />
        <span>{{ $t('profile.changePassword') }}</span>
      </button>
    </div>

    <form v-if="isChangingPassword" class="space-y-3" @submit.prevent="savePassword">
      <FormField v-slot="{ id }" :label="$t('profile.currentPassword')">
        <PasswordInput :id="id" v-model="currentPassword" autocomplete="current-password" />
      </FormField>
      <FormField v-if="needsCode" v-slot="{ id }" :label="$t('security.twoFactor.code')">
        <input :id="id" v-model="code" type="text" required autocomplete="one-time-code" autocapitalize="none" spellcheck="false" maxlength="32" class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 font-mono text-sm tracking-widest text-mnema-text" data-testid="password-code" />
      </FormField>
      <FormField v-slot="{ id }" :label="$t('profile.newPassword', { count: MIN_PASSWORD_LENGTH })">
        <PasswordInput :id="id" v-model="newPassword" autocomplete="new-password" />
      </FormField>
      <FormField v-slot="{ id }" :label="$t('profile.repeatPassword')">
        <PasswordInput :id="id" v-model="newPasswordRepeat" autocomplete="new-password" />
      </FormField>
      <div class="flex justify-end items-center gap-2 pt-1">
        <button
          type="button"
          @click="isChangingPassword = false; currentPassword = newPassword = newPasswordRepeat = code = ''"
          class="btn btn-ghost"
        >
          {{ $t('common.cancel') }}
        </button>
        <button
          type="submit"
          :disabled="isSavingPassword"
          class="btn btn-primary"
        >
          <Loader2 v-if="isSavingPassword" class="w-3.5 h-3.5 animate-spin" />
          <Save v-else class="w-3.5 h-3.5" />
          <span>{{ $t('common.save') }}</span>
        </button>
      </div>
    </form>

    <div v-if="passwordError" role="alert" class="p-2.5 rounded-md bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm flex items-center gap-1.5">
      <span>{{ passwordError }}</span>
    </div>
  </section>
</template>