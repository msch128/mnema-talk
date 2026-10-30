<script setup lang="ts">
// "Notifications & install": install Mnema Talk on this device and get notified while it is closed.
// What notifies is chosen elsewhere (per channel and for the whole server); a link leads there.
import { Bell } from '@lucide/vue'
import { notifyDialogOpen } from '../lib/notifyDialog'
import { useAccountStore } from '../stores/account'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import AppInstallSection from './AppInstallSection.vue'
import PushSection from './PushSection.vue'

const features = useServerFeaturesStore()
const account = useAccountStore()

function chooseWhatNotifies() {
  account.close()
  notifyDialogOpen.value = true
}
</script>

<template>
  <div class="space-y-6">
    <section v-if="features.notifyLevelsV2" class="settings-card flex flex-wrap items-center justify-between gap-3" data-testid="notify-link">
      <p class="settings-hint min-w-0 flex-1">{{ $t('settings.notifyLinkHint') }}</p>
      <button type="button" class="btn btn-secondary shrink-0" data-testid="notify-link-open" @click="chooseWhatNotifies">
        <Bell class="h-4 w-4" aria-hidden="true" />{{ $t('settings.notifyLink') }}
      </button>
    </section>
    <AppInstallSection />
    <PushSection />
  </div>
</template>
