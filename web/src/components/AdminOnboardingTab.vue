<script setup lang="ts">
import { charCount } from '../lib/textLength'
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
import { ImagePlus, ScrollText, Trash2 } from '@lucide/vue'
import { t } from '../i18n'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { IMAGE_UPLOAD_MAX_MB, pickImage } from '../lib/pickImage'
import { communityIconUrl } from '../lib/communityIcon'
import MarkdownContent from './MarkdownContent.vue'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

// Until a name is set, the address stands in for it (also in the sidebar header).
const host = window.location.hostname
const MAX_NAME = 60
const MAX_WELCOME = 4000
const MAX_RULES = 10000

const chat = useChatStore()
const toasts = useToastStore()
const onboarding = useOnboardingStore()
const features = useServerFeaturesStore()

const loaded = ref<OnboardingSettings | null>(null)
const previewing = ref<'' | 'welcome' | 'rules'>('')
const loadFailed = ref(false)

// The icon is stored on its own, as soon as it is picked (like avatars and banners), apart
// from the draft below: it is not part of Save, and a change here leaves unsaved edits alone.
const iconVersion = ref('')
const iconInput = ref<HTMLInputElement | null>(null)
const iconBusy = ref(false)
// The server crops uploads to the centre square.
const iconUrl = computed(() => communityIconUrl(iconVersion.value, 128))

interface CommunityForm {
  name: string
  welcome: string
  rules: string
  startChannel: string
  reaccept: boolean
}

// Everything on this screen is one draft; the community is changed by Save only (doc/settings-ux.md).
const form = useSettingsDraft<CommunityForm>({
  source: () => ({
    name: loaded.value?.community_name ?? '',
    welcome: loaded.value?.welcome_text ?? '',
    rules: loaded.value?.rules_text ?? '',
    startChannel: loaded.value?.start_channel_id ?? '',
    reaccept: false
  }),
  normalisers: {
    name: normalise.text,
    // Re-acceptance only counts while there are rules to accept, as only then it is sent.
    reaccept: (value, state) => value && state.rules.trim() !== ''
  },
  validate: draft => {
    const found: Partial<Record<keyof CommunityForm, string>> = {}
    if (charCount(draft.name) > MAX_NAME || charCount(draft.welcome) > MAX_WELCOME || charCount(draft.rules) > MAX_RULES) found.name = t('admin.onboarding.tooLong')
    return found
  },
  blocked: () => !loaded.value,
  save: async ({ draft, changed, commit }) => {
    loaded.value = await api('/api/admin/onboarding', {
      method: 'PUT',
      json: {
        community_name: draft.name, welcome_text: draft.welcome, rules_text: draft.rules,
        start_channel_id: draft.startChannel || null, require_reacceptance: draft.reaccept && draft.rules.trim() !== ''
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

async function load() {
  loadFailed.value = false
  try {
    loaded.value = await api('/api/admin/onboarding', { decode: decodeOnboardingSettings })
    iconVersion.value = loaded.value.community_icon ?? ''
  } catch (e) {
    loadFailed.value = true
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  }
}

function adoptIcon(s: OnboardingSettings) {
  iconVersion.value = s.community_icon ?? ''
  features.setCommunityIcon(iconVersion.value)
}

async function onIconSelected(e: Event) {
  const { file, tooBig } = pickImage(e.target as HTMLInputElement)
  if (tooBig) toasts.error(t('admin.onboarding.iconTooBig', { size: IMAGE_UPLOAD_MAX_MB }))
  if (!file) return
  const body = new FormData()
  body.append('icon', file)
  iconBusy.value = true
  try {
    adoptIcon(await api('/api/admin/community/icon', { method: 'PUT', form: body, decode: decodeOnboardingSettings }))
    toasts.success(t('admin.onboarding.iconSaved'))
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('admin.onboarding.iconFailed')))
  } finally {
    iconBusy.value = false
  }
}

async function removeIcon() {
  iconBusy.value = true
  try {
    adoptIcon(await api('/api/admin/community/icon', { method: 'DELETE', decode: decodeOnboardingSettings }))
    toasts.success(t('admin.onboarding.iconRemoved'))
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('admin.onboarding.iconFailed')))
  } finally {
    iconBusy.value = false
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
    <h3 class="flex items-center gap-2 text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider">
      <ScrollText class="w-4 h-4 text-mnema-accent" />
      <span>{{ $t('admin.tabs.onboarding') }}</span>
    </h3>
    <p class="text-xs text-mnema-tertiary">{{ $t('admin.onboarding.hint') }}</p>

    <div class="space-y-2" data-testid="community-icon">
      <span class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.icon') }}</span>
      <div class="flex items-center gap-3">
        <img v-if="iconUrl" :src="iconUrl" alt="" class="h-16 w-16 shrink-0 rounded-xl border border-mnema-hairline object-cover" data-testid="community-icon-preview" />
        <span v-else class="flex h-16 w-16 shrink-0 items-center justify-center rounded-xl border border-mnema-mint/30 bg-mnema-band text-xl font-semibold text-mnema-mint" aria-hidden="true">M</span>
        <div class="flex flex-wrap items-center gap-2">
          <button type="button" class="btn btn-secondary" :disabled="iconBusy" :aria-busy="iconBusy" data-testid="community-icon-change" @click="iconInput?.click()">
            <ImagePlus class="h-4 w-4" aria-hidden="true" />
            <span>{{ iconUrl ? $t('admin.onboarding.iconChange') : $t('admin.onboarding.iconUpload') }}</span>
          </button>
          <button v-if="iconUrl" type="button" class="btn btn-secondary" :disabled="iconBusy" data-testid="community-icon-remove" @click="removeIcon">
            <Trash2 class="h-4 w-4" aria-hidden="true" />
            <span>{{ $t('admin.onboarding.iconRemove') }}</span>
          </button>
          <input ref="iconInput" type="file" accept="image/png,image/jpeg,image/webp,image/gif" class="hidden" data-testid="community-icon-input" @change="onIconSelected" />
        </div>
      </div>
      <p class="text-xs text-mnema-tertiary">{{ $t('admin.onboarding.iconHint') }}</p>
    </div>

    <div class="space-y-1">
      <label for="onb-name" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.name') }}</label>
      <input id="onb-name" v-model="draft.name" type="text" v-max-chars="MAX_NAME" :placeholder="host" class="w-full max-w-sm rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" />
      <p class="text-xs text-mnema-tertiary">{{ $t('admin.onboarding.nameHint', { host }) }}</p>
    </div>

    <div class="space-y-1">
      <div class="flex items-center justify-between">
        <label for="onb-welcome" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.welcome') }}</label>
        <button type="button" class="text-xs text-mnema-accent hover:underline" :aria-pressed="previewing === 'welcome' ? 'true' : 'false'" @click="togglePreview('welcome')">{{ previewing === 'welcome' ? $t('admin.onboarding.edit') : $t('admin.onboarding.preview') }}</button>
      </div>
      <div v-if="previewing === 'welcome'" class="min-h-24 rounded-md border border-mnema-hairline bg-mnema-canvas/40 p-3 text-sm" data-testid="welcome-preview"><MarkdownContent :content="draft.welcome" /></div>
      <textarea v-else id="onb-welcome" v-model="draft.welcome" rows="5" v-max-chars="MAX_WELCOME" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" />
    </div>

    <div class="space-y-1">
      <div class="flex items-center justify-between">
        <label for="onb-rules" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.rules') }}</label>
        <button type="button" class="text-xs text-mnema-accent hover:underline" :aria-pressed="previewing === 'rules' ? 'true' : 'false'" @click="togglePreview('rules')">{{ previewing === 'rules' ? $t('admin.onboarding.edit') : $t('admin.onboarding.preview') }}</button>
      </div>
      <div v-if="previewing === 'rules'" class="min-h-24 rounded-md border border-mnema-hairline bg-mnema-canvas/40 p-3 text-sm" data-testid="rules-preview"><MarkdownContent :content="draft.rules" /></div>
      <textarea v-else id="onb-rules" v-model="draft.rules" rows="8" v-max-chars="MAX_RULES" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" />
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

    <p v-if="tooLong" class="text-xs text-mnema-danger" role="alert">{{ $t('admin.onboarding.tooLong') }}</p>
    <UnsavedChangesBar sticky save-testid="onboarding-save" :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
  </form>
</template>
