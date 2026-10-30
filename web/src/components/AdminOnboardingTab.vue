<script setup lang="ts">
// Owner and Administrators: the community's name, the welcome text, the rules
// and the channel new members land in. Rules are Markdown; once they exist,
// members must accept them before they may post, react, upload or join a Talk
// (the Owner and bots are exempt). "Require re-acceptance" raises the version.
// One draft, one Save.
import { ref, computed, onMounted } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeOnboardingSettings, type OnboardingSettings } from '../types/rest'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { useOnboardingStore } from '../stores/onboarding'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { t } from '../i18n'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import MarkdownContent from './MarkdownContent.vue'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

const MAX_NAME = 60
const MAX_WELCOME = 4000
const MAX_RULES = 10000
const MIN_DAYS = 1
const MAX_DAYS = 365

const chat = useChatStore()
const toasts = useToastStore()
const onboarding = useOnboardingStore()
const features = useServerFeaturesStore()

const loaded = ref<OnboardingSettings | null>(null)
const previewing = ref<'' | 'welcome' | 'rules'>('')
const loadFailed = ref(false)

interface CommunityForm {
  name: string
  welcome: string
  rules: string
  startChannel: string
  reaccept: boolean
  dmEnabled: boolean
  inactivityDays: number
}

// Everything on this screen is one draft; the community is changed by Save only (doc/settings-ux.md).
const form = useSettingsDraft<CommunityForm>({
  source: () => ({
    name: loaded.value?.community_name ?? '',
    welcome: loaded.value?.welcome_text ?? '',
    rules: loaded.value?.rules_text ?? '',
    startChannel: loaded.value?.start_channel_id ?? '',
    reaccept: false,
    dmEnabled: loaded.value?.direct_messages_enabled ?? true,
    inactivityDays: loaded.value?.session_inactivity_days ?? 7
  }),
  normalisers: {
    name: normalise.text,
    // Re-acceptance only counts while there are rules to accept, as only then it is sent.
    reaccept: (value, state) => value && state.rules.trim() !== '',
    // Without the direct messages feature the switch is not shown and never changes.
    dmEnabled: value => (features.dm ? value : true)
  },
  validate: draft => {
    const found: Partial<Record<keyof CommunityForm, string>> = {}
    if (draft.name.length > MAX_NAME || draft.welcome.length > MAX_WELCOME || draft.rules.length > MAX_RULES) found.name = t('admin.onboarding.tooLong')
    if (!Number.isInteger(draft.inactivityDays) || draft.inactivityDays < MIN_DAYS || draft.inactivityDays > MAX_DAYS) found.inactivityDays = t('admin.onboarding.inactivityRange', { min: MIN_DAYS, max: MAX_DAYS })
    return found
  },
  blocked: () => !loaded.value,
  save: async ({ draft, changed, commit }) => {
    loaded.value = await api('/api/admin/onboarding', {
      method: 'PUT',
      json: {
        community_name: draft.name, welcome_text: draft.welcome, rules_text: draft.rules,
        start_channel_id: draft.startChannel || null, require_reacceptance: draft.reaccept && draft.rules.trim() !== '',
        session_inactivity_days: draft.inactivityDays,
        ...(features.dm ? { direct_messages_enabled: draft.dmEnabled } : {})
      },
      decode: decodeOnboardingSettings
    })
    // The box is a one-off request: it is cleared once the rules were re-announced.
    commit(changed, { reaccept: false })
    draft.reaccept = false
  },
  saved: async () => {
    toasts.success(t('admin.onboarding.saved'))
    // The settings are saved; a failed refresh of the shared copy is no save error.
    await onboarding.reload().catch(() => {})
  }
})
const { draft, errors } = form

const textChannels = computed(() => chat.allChannels.filter(c => c.type === 'text'))
const hasRules = computed(() => draft.rules.trim() !== '')
const tooLong = computed(() => !!errors.value.name)
const daysInvalid = computed(() => !!errors.value.inactivityDays)

async function load() {
  loadFailed.value = false
  try {
    loaded.value = await api('/api/admin/onboarding', { decode: decodeOnboardingSettings })
  } catch (e) {
    loadFailed.value = true
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  }
}

function togglePreview(which: 'welcome' | 'rules') {
  previewing.value = previewing.value === which ? '' : which
}

onMounted(load)
</script>

<template>
  <div v-if="!loaded && loadFailed" class="space-y-3" role="alert" data-testid="onboarding-load-failed">
    <p class="text-sm text-mnema-danger">{{ $t('admin.onboarding.loadFailed') }}</p>
    <button type="button" class="btn btn-secondary" data-testid="onboarding-retry" @click="load">{{ $t('common.retry') }}</button>
  </div>
  <form v-else-if="loaded" class="space-y-5" data-testid="onboarding-settings" @submit.prevent="form.save" @keydown="form.onKeydown">
    <p class="text-xs text-mnema-tertiary">{{ $t('admin.onboarding.hint') }}</p>

    <div class="space-y-1">
      <label for="onb-name" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.name') }}</label>
      <input id="onb-name" v-model="draft.name" type="text" :maxlength="MAX_NAME" :placeholder="$t('admin.onboarding.namePlaceholder')" class="w-full max-w-sm rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" />
      <p class="text-xs text-mnema-tertiary">{{ $t('admin.onboarding.nameHint') }}</p>
    </div>

    <div class="space-y-1">
      <div class="flex items-center justify-between">
        <label for="onb-welcome" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.welcome') }}</label>
        <button type="button" class="text-xs text-mnema-accent hover:underline" :aria-pressed="previewing === 'welcome' ? 'true' : 'false'" @click="togglePreview('welcome')">{{ previewing === 'welcome' ? $t('admin.onboarding.edit') : $t('admin.onboarding.preview') }}</button>
      </div>
      <div v-if="previewing === 'welcome'" class="min-h-24 rounded-md border border-mnema-hairline bg-mnema-canvas/40 p-3 text-sm" data-testid="welcome-preview"><MarkdownContent :content="draft.welcome" /></div>
      <textarea v-else id="onb-welcome" v-model="draft.welcome" rows="5" :maxlength="MAX_WELCOME" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" />
    </div>

    <div class="space-y-1">
      <div class="flex items-center justify-between">
        <label for="onb-rules" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.rules') }}</label>
        <button type="button" class="text-xs text-mnema-accent hover:underline" :aria-pressed="previewing === 'rules' ? 'true' : 'false'" @click="togglePreview('rules')">{{ previewing === 'rules' ? $t('admin.onboarding.edit') : $t('admin.onboarding.preview') }}</button>
      </div>
      <div v-if="previewing === 'rules'" class="min-h-24 rounded-md border border-mnema-hairline bg-mnema-canvas/40 p-3 text-sm" data-testid="rules-preview"><MarkdownContent :content="draft.rules" /></div>
      <textarea v-else id="onb-rules" v-model="draft.rules" rows="8" :maxlength="MAX_RULES" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" />
      <p class="text-xs text-mnema-tertiary">{{ $t(hasRules ? 'admin.onboarding.rulesOn' : 'admin.onboarding.rulesOff', { version: loaded.rules_version }) }}</p>
      <label v-if="hasRules && loaded.rules_version > 0" class="flex items-center gap-2 text-sm text-mnema-text">
        <input v-model="draft.reaccept" type="checkbox" data-testid="reaccept" />
        <span>{{ $t('admin.onboarding.reaccept') }}</span>
      </label>
    </div>

    <div class="space-y-1">
      <label for="onb-start" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.startChannel') }}</label>
      <select id="onb-start" v-model="draft.startChannel" class="w-full max-w-sm rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent">
        <option value="">{{ $t('admin.onboarding.startChannelNone') }}</option>
        <option v-for="c in textChannels" :key="c.id" :value="c.id"># {{ c.name }}</option>
      </select>
    </div>

    <div v-if="features.dm" class="space-y-1" data-testid="dm-switch">
      <label class="flex items-center gap-2 text-sm text-mnema-text">
        <input v-model="draft.dmEnabled" type="checkbox" data-testid="dm-enabled" />
        <span>{{ $t('admin.onboarding.dmEnabled') }}</span>
      </label>
      <p class="text-xs text-mnema-tertiary">{{ $t('admin.onboarding.dmEnabledHint') }}</p>
    </div>

    <div class="space-y-1" data-testid="inactivity-setting">
      <label for="onb-inactivity" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.inactivity') }}</label>
      <input id="onb-inactivity" v-model.number="draft.inactivityDays" type="number" :min="MIN_DAYS" :max="MAX_DAYS" step="1" inputmode="numeric" :aria-invalid="daysInvalid ? 'true' : 'false'" class="w-28 rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" data-testid="inactivity-days" />
      <p class="text-xs text-mnema-tertiary">{{ $t('admin.onboarding.inactivityHint') }}</p>
      <p v-if="daysInvalid" class="text-xs text-mnema-danger" role="alert">{{ errors.inactivityDays }}</p>
    </div>

    <p v-if="tooLong" class="text-xs text-mnema-danger" role="alert">{{ $t('admin.onboarding.tooLong') }}</p>
    <UnsavedChangesBar sticky save-testid="onboarding-save" :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
  </form>
</template>
