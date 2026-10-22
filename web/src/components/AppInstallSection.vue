<script setup lang="ts">
import { computed } from 'vue'
import { installState, promptInstall } from '../lib/pwa'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

// Installing Mnema Talk as an app on this device: the browser's own prompt
// where there is one, and the Share-menu steps on iPhone and iPad.
const toasts = useToastStore()
const state = computed(installState)

async function install() {
  if (await promptInstall()) toasts.success(t('appSettings.installed'))
}
</script>

<template>
  <section class="space-y-2" data-testid="app-install">
    <h3 class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">{{ $t('appSettings.installTitle') }}</h3>
    <div class="rounded-lg bg-mnema-surface border border-mnema-hairline p-3.5 space-y-2">
      <p class="text-xs text-mnema-tertiary leading-relaxed" data-testid="app-install-state">{{ $t(`appSettings.install_${state}`) }}</p>
      <button
        v-if="state === 'available'"
        type="button"
        data-testid="app-install-button"
        class="px-4 py-2 rounded-lg bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold text-sm transition shadow-sm"
        @click="install"
      >{{ $t('appSettings.installAction') }}</button>
    </div>
  </section>
</template>
