<script setup lang="ts">
import { ref } from 'vue'
import { Edit3, Loader2, Save } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import { MIN_PASSWORD_LENGTH } from '../lib/passwordPolicy'

// Changing one's own password (own profile only).
const authStore = useAuthStore()
const toasts = useToastStore()

const isChangingPassword = ref(false)
const currentPassword = ref('')
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
    await authStore.changePassword(currentPassword.value, newPassword.value)
    currentPassword.value = newPassword.value = newPasswordRepeat.value = ''
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
  <div class="mb-3">
    <div class="flex items-center justify-between mb-1">
      <span class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('profile.password') }}</span>
      <button
        v-if="!isChangingPassword"
        @click="isChangingPassword = true; passwordError = ''"
        class="text-xs text-mnema-accent hover:underline flex items-center gap-1"
      >
        <Edit3 class="w-3.5 h-3.5" />
        <span>{{ $t('profile.changePassword') }}</span>
      </button>
    </div>

    <form v-if="isChangingPassword" class="space-y-2 mt-1" @submit.prevent="savePassword">
      <input
        v-model="currentPassword"
        type="password"
        autocomplete="current-password"
        :placeholder="$t('profile.currentPassword')" :aria-label="$t('profile.currentPassword')"
        class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent"
      />
      <input
        v-model="newPassword"
        type="password"
        autocomplete="new-password"
        :placeholder="$t('profile.newPassword', { count: MIN_PASSWORD_LENGTH })" :aria-label="$t('profile.newPassword', { count: MIN_PASSWORD_LENGTH })"
        class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent"
      />
      <input
        v-model="newPasswordRepeat"
        type="password"
        autocomplete="new-password"
        :placeholder="$t('profile.repeatPassword')" :aria-label="$t('profile.repeatPassword')"
        class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent"
      />
      <div class="flex justify-end items-center gap-1.5 text-xs">
        <button
          type="button"
          @click="isChangingPassword = false; currentPassword = newPassword = newPasswordRepeat = ''"
          class="px-2 py-0.5 rounded text-mnema-muted hover:text-mnema-text"
        >
          {{ $t('common.cancel') }}
        </button>
        <button
          type="submit"
          :disabled="isSavingPassword"
          class="px-2.5 py-1 rounded bg-mnema-accent text-mnema-canvas font-bold flex items-center gap-1 hover:brightness-110 active:scale-95 disabled:opacity-50"
        >
          <Loader2 v-if="isSavingPassword" class="w-3.5 h-3.5 animate-spin" />
          <Save v-else class="w-3.5 h-3.5" />
          <span>{{ $t('common.save') }}</span>
        </button>
      </div>
    </form>

    <div v-if="passwordError" role="alert" class="mt-2 p-1.5 rounded bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm flex items-center gap-1.5">
      <span>{{ passwordError }}</span>
    </div>
  </div>
</template>
