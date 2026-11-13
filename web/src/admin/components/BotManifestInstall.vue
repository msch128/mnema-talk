<script setup lang="ts">
// Adding a bot from a manifest (doc/bots.md): paste it (or arrive by an add
// link), the server checks it, the confirm screen shows what it asks for, and
// "Add bot" creates it. Afterwards the avatar from the manifest is uploaded and
// the token is shown once by the Bots tab. A wizard: it does something once, so
// it has one primary action per step and asks before typed input is dropped.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { FileJson } from '@lucide/vue'
import { api, caughtErrorMessage } from '../../shared/lib/api'
import { decodeBotBot, decodeBotCheckedManifest, decodeBotInstallResponse } from '../../types/rest'
import type { BotBot, BotCheckedManifest } from '../../types/rest'
import { confirmDiscard, useLeaveGuard } from '../../shared/composables/draftGuard'
import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'
import { avatarFromManifest, initialPermissions, manifestTextFromParam, readManifest, type AvatarRead } from '../lib/manifest'
import BotManifestConfirm from './BotManifestConfirm.vue'

const props = defineProps({
  /** The base64url manifest of an add link, checked at once. */
  linked: { type: String, default: '' }
})
const emit = defineEmits<{ installed: [bot: BotBot, secret: string]; cancel: [] }>()
const toasts = useToastStore()

const text = ref('')
const manifest = ref<Record<string, unknown> | null>(null)
const checked = ref<BotCheckedManifest | null>(null)
const avatar = ref<AvatarRead>({ file: null, problem: null })
const avatarUrl = ref('')
const intents = ref<string[]>([])
const permissions = ref<string[]>([])
const channels = ref<string[]>([])
const busy = ref(false)
const error = ref('')

const dirty = computed(() => checked.value !== null || text.value.trim() !== '')
useLeaveGuard(dirty)

async function cancel() {
  if (dirty.value && !(await confirmDiscard())) return
  text.value = ''
  checked.value = null
  emit('cancel')
}

/** For the Bots tab: may the flow be replaced (another bot opened)? */
async function guard(): Promise<boolean> {
  return !dirty.value || confirmDiscard()
}
defineExpose({ guard })

watch(avatar, (next) => {
  if (avatarUrl.value) URL.revokeObjectURL(avatarUrl.value)
  avatarUrl.value = 'file' in next && next.file ? URL.createObjectURL(next.file) : ''
})
onBeforeUnmount(() => { if (avatarUrl.value) URL.revokeObjectURL(avatarUrl.value) })

function readFailure(reason: 'json' | 'object'): string {
  return t(reason === 'json' ? 'bots.manifest.notJson' : 'bots.manifest.notObject')
}

const heading = ref<HTMLElement | null>(null)

// Arriving by an add link, the confirm screen may start below the list of bots (a phone, a
// narrow pane): it is scrolled into view and its heading takes the focus.
async function showConfirm() {
  await nextTick()
  heading.value?.scrollIntoView?.({ block: 'start', behavior: 'smooth' })
  heading.value?.focus({ preventScroll: true })
}

async function check() {
  if (busy.value) return
  const read = readManifest(text.value.trim())
  if (!read.ok) {
    error.value = readFailure(read.reason)
    return
  }
  busy.value = true
  error.value = ''
  try {
    const result = await api('/api/admin/bots/manifests/check', { method: 'POST', json: read.manifest, decode: decodeBotCheckedManifest })
    manifest.value = read.manifest
    checked.value = result
    if (props.linked) void showConfirm()
    intents.value = result.intents.map(i => i.name)
    permissions.value = initialPermissions(result.permissions)
    channels.value = []
    avatar.value = avatarFromManifest(read.manifest, result.name)
  } catch (e) {
    error.value = caughtErrorMessage(e, t('admin.unknownError'))
  } finally {
    busy.value = false
  }
}

function back() {
  checked.value = null
  manifest.value = null
  avatar.value = { file: null, problem: null }
  error.value = ''
}

// The bot exists once install answers; a failed avatar upload only says so.
async function uploadAvatar(bot: BotBot): Promise<BotBot> {
  if (!('file' in avatar.value) || !avatar.value.file) return bot
  const body = new FormData()
  body.append('avatar', avatar.value.file)
  try {
    return await api(`/api/admin/bots/${bot.id}/avatar`, { method: 'POST', form: body, decode: decodeBotBot })
  } catch {
    toasts.error(t('bots.manifest.avatarFailed'))
    return bot
  }
}

async function install() {
  if (busy.value || !manifest.value || !checked.value) return
  busy.value = true
  error.value = ''
  try {
    const json = { manifest: manifest.value, permissions: permissions.value, intents: intents.value, channel_ids: channels.value }
    const result = await api('/api/admin/bots/install', { method: 'POST', json, decode: decodeBotInstallResponse })
    const bot = await uploadAvatar(result.bot)
    // Done: nothing is left to lose, so leaving no longer asks.
    checked.value = null
    text.value = ''
    toasts.success(t('bots.manifest.installed', { name: bot.name }))
    emit('installed', bot, result.token.secret)
  } catch (e) {
    error.value = caughtErrorMessage(e, t('admin.unknownError'))
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  if (!props.linked) return
  const decoded = manifestTextFromParam(props.linked)
  if (decoded === null) {
    error.value = t('bots.manifest.badLink')
    return
  }
  text.value = decoded
  void check()
})
</script>

<template>
  <section class="min-w-0 stack-4" :aria-labelledby="'manifest-title'" data-testid="manifest-install-flow">
    <h4 id="manifest-title" ref="heading" tabindex="-1" class="flex items-center gap-2 text-sm font-semibold text-mnema-text outline-hidden">
      <FileJson class="h-4 w-4 shrink-0 text-mnema-accent" aria-hidden="true" />
      <span class="min-w-0">{{ checked ? $t('bots.manifest.confirmTitle') : $t('bots.manifest.title') }}</span>
    </h4>

    <BotManifestConfirm
      v-if="checked"
      v-model:intents="intents"
      v-model:permissions="permissions"
      v-model:channels="channels"
      :manifest="checked"
      :avatar-url="avatarUrl"
      :avatar-problem="'problem' in avatar ? avatar.problem : null"
      :busy="busy"
      :error="error"
      @back="back"
      @submit="install"
    />

    <form v-else class="stack-3" data-testid="manifest-paste" @submit.prevent="check">
      <p class="text-sm text-mnema-tertiary">{{ $t('bots.manifest.intro') }}</p>
      <label class="stack-1">
        <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('bots.manifest.pasteLabel') }}</span>
        <textarea
          v-model="text"
          rows="8"
          spellcheck="false"
          :aria-invalid="error ? 'true' : undefined"
          :aria-describedby="error ? 'manifest-paste-error' : undefined"
          class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 font-mono text-xs text-mnema-text outline-hidden focus:border-mnema-accent"
          data-testid="manifest-text"
        />
      </label>
      <p v-if="error" id="manifest-paste-error" role="alert" class="text-sm wrap-break-word text-mnema-danger" data-testid="manifest-check-error">{{ error }}</p>
      <div class="flex flex-wrap items-center justify-end gap-2">
        <button type="button" class="btn btn-ghost" :disabled="busy" data-testid="manifest-cancel" @click="cancel">{{ $t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary" :disabled="busy || !text.trim()" :aria-busy="busy ? 'true' : undefined" data-testid="manifest-check">{{ $t('bots.manifest.check') }}</button>
      </div>
    </form>
  </section>
</template>
