<script setup lang="ts">
// The "Direct messages" section of the privacy settings: who may start a
// conversation with the member, and whether all of them are muted.
import { ref } from 'vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import AccountDmPolicy from './AccountDmPolicy.vue'

const chat = useChatStore()
const features = useServerFeaturesStore()
const toasts = useToastStore()
const busy = ref(false)

async function toggleMuteAll() {
  if (busy.value) return
  busy.value = true
  try {
    await chat.saveDmSettings({ mute_all: !chat.dmSettings.mute_all })
  } catch {
    toasts.error(t('dm.policy.failed'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="settings-sub" data-testid="account-dm">
    <p class="settings-subtitle">{{ $t('dm.settings.title') }}</p>
    <AccountDmPolicy />
    <section v-if="features.dmV2" class="mt-4 flex items-start justify-between gap-6">
      <div class="settings-row-text">
        <div id="dm-mute-all-title" class="settings-row-title">{{ $t('dm.settings.muteAll') }}</div>
        <p class="settings-hint">{{ $t('dm.settings.muteAllHint') }}</p>
      </div>
      <button
        type="button"
        role="switch"
        :aria-checked="chat.dmSettings.mute_all ? 'true' : 'false'"
        aria-labelledby="dm-mute-all-title"
        :disabled="busy"
        class="switch mt-0.5"
        data-testid="dm-mute-all"
        @click="toggleMuteAll"
      ></button>
    </section>
  </div>
</template>
