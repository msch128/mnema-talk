<script setup lang="ts">
// "Privacy": who may write to this member and whom they blocked (each only on
// a server that has the feature). Export and deletion live under Data & account.
import { useServerFeaturesStore } from '../stores/serverFeatures'
import AccountDirectMessages from './AccountDirectMessages.vue'
import AccountBlockedList from './AccountBlockedList.vue'

const features = useServerFeaturesStore()
</script>

<template>
  <div class="space-y-5" data-testid="account-privacy-settings">
    <AccountDirectMessages v-if="features.dm" />
    <AccountBlockedList v-if="features.blocks" />
    <p v-if="!features.dm && !features.blocks" class="text-sm text-mnema-tertiary" data-testid="privacy-empty">{{ $t('settings.privacyEmpty') }}</p>
  </div>
</template>
