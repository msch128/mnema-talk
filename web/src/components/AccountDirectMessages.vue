<script setup lang="ts">
// The "Direct messages" section of the privacy settings: who may start a
// conversation with the member, and whether all of them are muted.
import { ref } from 'vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { usePreferenceSave } from '../composables/usePreferenceSave'
import { t } from '../i18n'
import SavedIndicator from './SavedIndicator.vue'
import AccountDmPolicy from './AccountDmPolicy.vue'
import SwitchRow from './SwitchRow.vue'

const chat = useChatStore()
const features = useServerFeaturesStore()
const pref = usePreferenceSave()
const busy = ref(false)

// The switch is disabled while a save runs, so no second call can start.
async function toggleMuteAll() {
  busy.value = true
  try {
    await pref.run(() => chat.saveDmSettings({ mute_all: !chat.dmSettings.mute_all }), t('dm.policy.failed'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="settings-card" data-testid="account-dm" aria-labelledby="dm-settings-title">
    <div class="flex items-center justify-between gap-3">
      <h3 id="dm-settings-title" class="settings-title">{{ $t('dm.settings.title') }}</h3>
      <SavedIndicator :visible="pref.saved.value" />
    </div>
    <AccountDmPolicy @saved="pref.flash" />
    <div v-if="features.dmV2" class="border-t border-mnema-hairline pt-4">
      <SwitchRow
        :model-value="chat.dmSettings.mute_all"
        :title="$t('dm.settings.muteAll')"
        :hint="$t('dm.settings.muteAllHint')"
        :disabled="busy"
        data-testid="dm-mute-all"
        @toggle="toggleMuteAll"
      />
    </div>
  </section>
</template>
