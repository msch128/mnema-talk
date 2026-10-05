<script setup>
// Shown to everyone when the server runs a newer (or just different) build
// than this page. Reloading is always the user's click: during a call the
// page reload rejoins the call automatically (voice session resume).
import { computed } from 'vue'
import { Sparkles, X } from '@lucide/vue'
import { useAppVersionStore } from '../stores/appVersion'
import { useVoiceStore } from '../stores/voice'

const versionStore = useAppVersionStore()
const voiceStore = useVoiceStore()

const inCall = computed(() => !!voiceStore.currentChannelId)

function reload() {
  window.location.reload()
}
</script>

<template>
  <div
    v-if="versionStore.showReloadBanner"
    role="status"
    data-testid="update-banner"
    class="flex min-h-10 flex-shrink-0 items-center gap-2.5 border-b border-mnema-accent/35 bg-mnema-accent/10 py-1.5 pl-4 pr-2 text-sm"
  >
    <Sparkles class="h-4 w-4 flex-shrink-0 text-mnema-accent" />
    <button
      type="button"
      data-testid="update-reload"
      class="min-w-0 flex-1 text-left text-mnema-text hover:underline"
      @click="reload"
    >
      <span class="font-medium">{{ $t('update.reloadBanner') }}</span>
      <span v-if="inCall" data-testid="update-call-note" class="ml-1.5 text-mnema-muted">{{ $t('update.reloadInCall') }}</span>
    </button>
    <button
      type="button"
      data-testid="update-dismiss"
      class="flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-md text-mnema-tertiary transition hover:bg-mnema-hover hover:text-mnema-text"
      :aria-label="$t('update.dismiss')"
      v-tooltip="$t('update.dismiss')"
      @click="versionStore.dismiss()"
    >
      <X class="h-4 w-4" />
    </button>
  </div>
</template>
