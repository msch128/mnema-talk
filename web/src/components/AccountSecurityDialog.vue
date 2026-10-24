<script setup lang="ts">
// "Account & security": sign-in security of the member's own account. Each
// topic is its own section; they all work on the loaded account state.
import { onMounted, ref } from 'vue'
import { caughtErrorMessage } from '../lib/api'
import { t } from '../i18n'
import { useAccountStore } from '../stores/account'
import ProfilePassword from './ProfilePassword.vue'
import AccountEmail from './AccountEmail.vue'
import AccountTwoFactor from './AccountTwoFactor.vue'
import AccountPrivacy from './AccountPrivacy.vue'
import AccountSessions from './AccountSessions.vue'
import BaseDialog from './BaseDialog.vue'

const account = useAccountStore()
const loadError = ref('')

onMounted(async () => {
  try {
    await account.load()
  } catch (err) {
    loadError.value = caughtErrorMessage(err, t('errors.unknown'))
  }
})
</script>

<template>
  <BaseDialog :title="$t('security.title')" :subtitle="$t('security.subtitle')" panel-class="max-w-xl" @close="account.close()">
    <div class="min-h-0 space-y-5 overflow-y-auto p-6 max-md:p-4" data-testid="account-security">
      <div v-if="loadError" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-2 text-sm text-mnema-danger">{{ loadError }}</div>
      <p v-else-if="!account.info" role="status" class="text-sm text-mnema-tertiary">{{ $t('security.loading') }}</p>
      <template v-else>
        <ProfilePassword />
        <AccountTwoFactor />
        <AccountEmail />
        <AccountSessions />
        <AccountPrivacy />
      </template>
    </div>
  </BaseDialog>
</template>
