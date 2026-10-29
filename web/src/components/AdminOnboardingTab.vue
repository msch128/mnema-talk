<script setup lang="ts">
// Owner and Administrators: the community's name, the welcome text, the rules
// and the channel new members land in. Rules are Markdown; once they exist,
// members must accept them before they may post, react, upload or join a Talk
// (the Owner and bots are exempt). "Require re-acceptance" raises the version.
import { ref, computed, onMounted } from 'vue'
import { ImagePlus, Trash2 } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeOnboardingSettings, type OnboardingSettings } from '../types/rest'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { useOnboardingStore } from '../stores/onboarding'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { t } from '../i18n'
import { IMAGE_UPLOAD_MAX_MB, pickImage } from '../lib/pickImage'
import { communityIconUrl } from '../lib/communityIcon'
import MarkdownContent from './MarkdownContent.vue'

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
const name = ref('')
const welcome = ref('')
const rules = ref('')
const startChannel = ref('')
const reaccept = ref(false)
// The community's switch for direct messages (existing conversations stay readable when it is off).
const dmEnabled = ref(true)
// Days without a request after which a session is signed out.
const inactivityDays = ref(7)
const previewing = ref<'' | 'welcome' | 'rules'>('')
const busy = ref(false)
const loadFailed = ref(false)
const iconInput = ref<HTMLInputElement | null>(null)
const iconBusy = ref(false)
// The stored icon, as the server serves it; the server crops uploads to the centre square.
const iconUrl = computed(() => communityIconUrl(loaded.value?.community_icon ?? '', 128))

const textChannels = computed(() => chat.allChannels.filter(c => c.type === 'text'))
const dirty = computed(() => !!loaded.value && (
  name.value !== loaded.value.community_name || welcome.value !== loaded.value.welcome_text ||
  rules.value !== loaded.value.rules_text || startChannel.value !== (loaded.value.start_channel_id ?? '') || reaccept.value ||
  (features.dm && dmEnabled.value !== loaded.value.direct_messages_enabled) ||
  inactivityDays.value !== loaded.value.session_inactivity_days
))
const daysInvalid = computed(() => !Number.isInteger(inactivityDays.value) || inactivityDays.value < MIN_DAYS || inactivityDays.value > MAX_DAYS)
const tooLong = computed(() => name.value.length > MAX_NAME || welcome.value.length > MAX_WELCOME || rules.value.length > MAX_RULES)
const hasRules = computed(() => rules.value.trim() !== '')

function apply(s: OnboardingSettings) {
  loaded.value = s
  name.value = s.community_name
  welcome.value = s.welcome_text
  rules.value = s.rules_text
  startChannel.value = s.start_channel_id ?? ''
  reaccept.value = false
  dmEnabled.value = s.direct_messages_enabled
  inactivityDays.value = s.session_inactivity_days
}

async function load() {
  loadFailed.value = false
  try {
    apply(await api('/api/admin/onboarding', { decode: decodeOnboardingSettings }))
  } catch (e) {
    loadFailed.value = true
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  }
}

async function save() {
  busy.value = true
  try {
    apply(await api('/api/admin/onboarding', {
      method: 'PUT',
      json: {
        community_name: name.value, welcome_text: welcome.value, rules_text: rules.value,
        start_channel_id: startChannel.value || null, require_reacceptance: reaccept.value && hasRules.value,
        session_inactivity_days: inactivityDays.value,
        ...(features.dm ? { direct_messages_enabled: dmEnabled.value } : {})
      },
      decode: decodeOnboardingSettings
    }))
    toasts.success(t('admin.onboarding.saved'))
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
    return
  } finally {
    busy.value = false
  }
  // The settings are saved; a failed refresh of the shared copy is no save error.
  await onboarding.reload().catch(() => {})
}

/** The icon is stored on its own, apart from the texts: unsaved edits above stay as they are. */
function adoptIcon(s: OnboardingSettings) {
  if (loaded.value) {
    const next = { ...loaded.value }
    if (s.community_icon) next.community_icon = s.community_icon
    else delete next.community_icon
    loaded.value = next
  }
  features.setCommunityIcon(s.community_icon ?? '')
}

async function onIconSelected(e: Event) {
  const { file, tooBig } = pickImage(e.target as HTMLInputElement)
  if (tooBig) toasts.error(t('admin.onboarding.iconTooBig', { size: IMAGE_UPLOAD_MAX_MB }))
  if (!file) return
  const form = new FormData()
  form.append('icon', file)
  iconBusy.value = true
  try {
    adoptIcon(await api('/api/admin/community/icon', { method: 'PUT', form, decode: decodeOnboardingSettings }))
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
  <form v-else-if="loaded" class="space-y-5" data-testid="onboarding-settings" @submit.prevent="save">
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
      <input id="onb-name" v-model="name" type="text" :maxlength="MAX_NAME" :placeholder="$t('admin.onboarding.namePlaceholder')" class="w-full max-w-sm rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" />
      <p class="text-xs text-mnema-tertiary">{{ $t('admin.onboarding.nameHint') }}</p>
    </div>

    <div class="space-y-1">
      <div class="flex items-center justify-between">
        <label for="onb-welcome" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.welcome') }}</label>
        <button type="button" class="text-xs text-mnema-accent hover:underline" :aria-pressed="previewing === 'welcome' ? 'true' : 'false'" @click="togglePreview('welcome')">{{ previewing === 'welcome' ? $t('admin.onboarding.edit') : $t('admin.onboarding.preview') }}</button>
      </div>
      <div v-if="previewing === 'welcome'" class="min-h-24 rounded-md border border-mnema-hairline bg-mnema-canvas/40 p-3 text-sm" data-testid="welcome-preview"><MarkdownContent :content="welcome" /></div>
      <textarea v-else id="onb-welcome" v-model="welcome" rows="5" :maxlength="MAX_WELCOME" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" />
    </div>

    <div class="space-y-1">
      <div class="flex items-center justify-between">
        <label for="onb-rules" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.rules') }}</label>
        <button type="button" class="text-xs text-mnema-accent hover:underline" :aria-pressed="previewing === 'rules' ? 'true' : 'false'" @click="togglePreview('rules')">{{ previewing === 'rules' ? $t('admin.onboarding.edit') : $t('admin.onboarding.preview') }}</button>
      </div>
      <div v-if="previewing === 'rules'" class="min-h-24 rounded-md border border-mnema-hairline bg-mnema-canvas/40 p-3 text-sm" data-testid="rules-preview"><MarkdownContent :content="rules" /></div>
      <textarea v-else id="onb-rules" v-model="rules" rows="8" :maxlength="MAX_RULES" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" />
      <p class="text-xs text-mnema-tertiary">{{ $t(hasRules ? 'admin.onboarding.rulesOn' : 'admin.onboarding.rulesOff', { version: loaded.rules_version }) }}</p>
      <label v-if="hasRules && loaded.rules_version > 0" class="flex items-center gap-2 text-sm text-mnema-text">
        <input v-model="reaccept" type="checkbox" data-testid="reaccept" />
        <span>{{ $t('admin.onboarding.reaccept') }}</span>
      </label>
    </div>

    <div class="space-y-1">
      <label for="onb-start" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.startChannel') }}</label>
      <select id="onb-start" v-model="startChannel" class="w-full max-w-sm rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent">
        <option value="">{{ $t('admin.onboarding.startChannelNone') }}</option>
        <option v-for="c in textChannels" :key="c.id" :value="c.id"># {{ c.name }}</option>
      </select>
    </div>

    <div v-if="features.dm" class="space-y-1" data-testid="dm-switch">
      <label class="flex items-center gap-2 text-sm text-mnema-text">
        <input v-model="dmEnabled" type="checkbox" data-testid="dm-enabled" />
        <span>{{ $t('admin.onboarding.dmEnabled') }}</span>
      </label>
      <p class="text-xs text-mnema-tertiary">{{ $t('admin.onboarding.dmEnabledHint') }}</p>
    </div>

    <div class="space-y-1" data-testid="inactivity-setting">
      <label for="onb-inactivity" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.onboarding.inactivity') }}</label>
      <input id="onb-inactivity" v-model.number="inactivityDays" type="number" :min="MIN_DAYS" :max="MAX_DAYS" step="1" inputmode="numeric" :aria-invalid="daysInvalid ? 'true' : 'false'" class="w-28 rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" data-testid="inactivity-days" />
      <p class="text-xs text-mnema-tertiary">{{ $t('admin.onboarding.inactivityHint') }}</p>
      <p v-if="daysInvalid" class="text-xs text-mnema-danger" role="alert">{{ $t('admin.onboarding.inactivityRange', { min: MIN_DAYS, max: MAX_DAYS }) }}</p>
    </div>

    <p v-if="tooLong" class="text-xs text-mnema-danger" role="alert">{{ $t('admin.onboarding.tooLong') }}</p>
    <button type="submit" :disabled="!dirty || busy || tooLong || daysInvalid" class="btn btn-primary" data-testid="onboarding-save">{{ $t('common.save') }}</button>
  </form>
</template>
