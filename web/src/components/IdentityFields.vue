<script setup lang="ts">
// The password and, while two-factor sign-in is on, the current code: what the
// server asks for before deleting an account, exporting its data or taking
// over another member's account.
import FormField from './FormField.vue'
import PasswordInput from './PasswordInput.vue'

defineProps<{ needsCode: boolean }>()
const password = defineModel<string>('password', { default: '' })
const code = defineModel<string>('code', { default: '' })
</script>

<template>
  <FormField v-slot="{ id }" :label="$t('profile.currentPassword')">
    <PasswordInput :id="id" v-model="password" required autocomplete="current-password" data-testid="privacy-password" />
  </FormField>
  <FormField v-if="needsCode" v-slot="{ id }" :label="$t('security.twoFactor.code')">
    <input :id="id" v-model="code" type="text" required autocomplete="one-time-code" autocapitalize="none" spellcheck="false" maxlength="32" class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 font-mono text-sm tracking-widest text-mnema-text" data-testid="privacy-code" />
  </FormField>
</template>
