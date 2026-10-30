<script setup lang="ts">
// Owner and Administrators: who may write direct messages and when an idle
// session is signed out. Both live in the onboarding settings on the server,
// which replaces name, texts and rules as a whole; a save therefore reads them
// fresh first, so it never writes back a stale copy of what the Welcome & rules
// page holds. One draft, one Save (doc/settings-ux.md).
import { ref, computed, onMounted } from 'vue'
import { KeyRound } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeOnboardingSettings, type OnboardingSettings } from '../types/rest'
import { useToastStore } from '../stores/toast'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { t } from '../i18n'
import { useSettingsDraft } from '../composables/useSettingsDraft'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

const MIN_DAYS = 1
const MAX_DAYS = 365

const toasts = useToastStore()
const features = useServerFeaturesStore()
const loaded = ref<OnboardingSettings | null>(null)

interface AccessForm {
  // The community's switch for direct messages (existing conversations stay readable when it is off).
  dmEnabled: boolean
  // Days without a request after which a session is signed out.
  inactivityDays: number
}

const form = useSettingsDraft<AccessForm>({
  source: () => ({
    dmEnabled: loaded.value?.direct_messages_enabled ?? true,
    inactivityDays: loaded.value?.session_inactivity_days ?? 7
  }),
  // Without the direct messages feature the switch is not shown and never changes.
  normalisers: { dmEnabled: value => (features.dm ? value : true) },
  validate: draft => Number.isInteger(draft.inactivityDays) && draft.inactivityDays >= MIN_DAYS && draft.inactivityDays <= MAX_DAYS
    ? {}
    : { inactivityDays: t('admin.onboarding.inactivityRange', { min: MIN_DAYS, max: MAX_DAYS }) },
  blocked: () => !loaded.value,
  save: async ({ draft }) => {
    const fresh = await api('/api/admin/onboarding', { decode: decodeOnboardingSettings })
    loaded.value = await api('/api/admin/onboarding', {
      method: 'PUT',
      json: {
        community_name: fresh.community_name, welcome_text: fresh.welcome_text, rules_text: fresh.rules_text,
        start_channel_id: fresh.start_channel_id ?? null, require_reacceptance: false,
        session_inactivity_days: draft.inactivityDays,
        ...(features.dm ? { direct_messages_enabled: draft.dmEnabled } : {})
      },
      decode: decodeOnboardingSettings
    })
  },
  saved: () => { toasts.success(t('admin.access.saved')) }
})
const { draft, errors } = form
const daysInvalid = computed(() => !!errors.value.inactivityDays)

async function load() {
  try {
    loaded.value = await api('/api/admin/onboarding', { decode: decodeOnboardingSettings })
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  }
}

onMounted(load)
</script>

<template>
  <form v-if="loaded" class="space-y-5" data-testid="access-settings" @submit.prevent="form.save" @keydown="form.onKeydown">
    <h3 class="flex items-center gap-2 text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider">
      <KeyRound class="w-4 h-4 text-mnema-accent" />
      <span>{{ $t('admin.tabs.access') }}</span>
    </h3>

    <div v-if="features.dm" class="space-y-1" data-testid="dm-switch">
      <label class="flex items-center gap-2 text-sm text-mnema-text">
        <input v-model="draft.dmEnabled" type="checkbox" data-testid="dm-enabled" />
        <span>{{ $t('admin.onboarding.dmEnabled') }}</span>
      </label>
      <p class="text-xs text-mnema-tertiary">{{ $t('admin.onboarding.dmEnabledHint') }}</p>
    </div>

    <div class="space-y-1" data-testid="inactivity-setting">
      <label for="access-inactivity" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.inactivity') }}</label>
      <input id="access-inactivity" v-model.number="draft.inactivityDays" type="number" :min="MIN_DAYS" :max="MAX_DAYS" step="1" inputmode="numeric" :aria-invalid="daysInvalid ? 'true' : 'false'" class="w-28 rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" data-testid="inactivity-days" />
      <p class="text-xs text-mnema-tertiary">{{ $t('admin.onboarding.inactivityHint') }}</p>
      <p v-if="daysInvalid" class="text-xs text-mnema-danger" role="alert">{{ errors.inactivityDays }}</p>
    </div>

    <UnsavedChangesBar sticky save-testid="access-save" :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
  </form>
</template>
