<script setup lang="ts">
// Owner and Administrators: messages with a lifetime (on or off, the longest
// one) and the community's auto-delete default (doc/timed-messages.md). Each
// panel is a draft of its own with one Save (doc/settings-ux.md).
import { onMounted, ref } from 'vue'
import { api, caughtErrorMessage } from '../../shared/lib/api'
import { decodeChatMessageSettingsView, type ChatMessageSettingsView } from '../../types/rest'
import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'
import AdminLifetimeSettings from './AdminLifetimeSettings.vue'
import AdminAutoDeleteDefault from './AdminAutoDeleteDefault.vue'

const toasts = useToastStore()
const settings = ref<ChatMessageSettingsView | null>(null)

async function load() {
  try {
    settings.value = await api('/api/admin/message-settings', { decode: decodeChatMessageSettingsView })
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  }
}
onMounted(load)
</script>

<template>
  <section class="space-y-4" data-testid="admin-messages">
    <header class="space-y-1">
      <h2 class="text-lg font-semibold text-mnema-text">{{ $t('admin.messages.title') }}</h2>
      <p class="text-sm text-mnema-tertiary">{{ $t('admin.messages.intro') }}</p>
    </header>
    <template v-if="settings">
      <AdminLifetimeSettings :settings="settings" @saved="settings = $event" />
      <AdminAutoDeleteDefault :settings="settings" @saved="settings = $event" />
    </template>
  </section>
</template>
