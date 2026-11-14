<script setup lang="ts">
// One bot: avatar, name, on/off, allowed intents and roles are one draft with one
// Save (doc/settings-ux.md); delete, tokens and commands are actions in their own
// sections after it, and the access preview only reads.
import { computed, ref } from 'vue'
import { ExternalLink, ImagePlus, Trash2 } from '@lucide/vue'
import { safeHomepage } from '../lib/manifest'
import { api, caughtErrorMessage } from '../../shared/lib/api'
import { decodeBotBot } from '../../types/rest'
import type { BotBot } from '../../types/rest'
import { confirm } from '../../shared/lib/confirm'
import { BOT_INTENTS, botInitial, hasIntent, toggleIntent } from '../lib/bots'
import { normalise, useSettingsDraft } from '../../shared/composables/useSettingsDraft'
import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'
import type { ImageChange } from '../../shared/composables/useStagedImage'
import { normaliseImage, useStagedImage } from '../../shared/composables/useStagedImage'
import BotRolesPanel from './BotRolesPanel.vue'
import UnsavedChangesBar from '../../shared/components/UnsavedChangesBar.vue'
import BotAccessPanel from './BotAccessPanel.vue'
import BotChannelAccessPanel from './BotChannelAccessPanel.vue'
import BotTokensPanel from './BotTokensPanel.vue'
import BotCommandsPanel from './BotCommandsPanel.vue'

const props = defineProps({
  bot: { type: Object as () => BotBot, required: true }
})
const emit = defineEmits<{ updated: [bot: BotBot]; deleted: [id: string] }>()

const toasts = useToastStore()
// Bumped when roles change so the access preview reloads.
const accessVersion = ref(0)
const base = `/api/admin/bots/${props.bot.id}`
// Description and homepage come from the manifest the bot was added from.
const homepage = computed(() => safeHomepage(props.bot.homepage))

function fail(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}

interface BotForm { avatar: ImageChange; name: string; enabled: boolean; intents: number; roles: string[] }

const form = useSettingsDraft<BotForm>({
  source: () => ({ avatar: null, name: props.bot.name, enabled: !props.bot.disabled, intents: props.bot.allowed_intents, roles: [...props.bot.role_ids] }),
  normalisers: { avatar: normaliseImage, name: normalise.text, enabled: normalise.flag, roles: normalise.set },
  validate: draft => (normalise.text(draft.name) ? {} : { name: t('bots.nameRequired') }),
  // Defined order: the avatar, the bot itself (name, switch, intents) in one call, then its roles.
  save: async ({ changed, draft, commit }) => {
    if (draft.avatar instanceof File) {
      const body = new FormData()
      body.append('avatar', draft.avatar)
      emit('updated', await api(`${base}/avatar`, { method: 'POST', form: body, decode: decodeBotBot }))
      commit(['avatar'], { avatar: null })
      draft.avatar = null
    }
    const json: { name?: string; allowed_intents?: number; disabled?: boolean } = {}
    if (changed.includes('name')) json.name = normalise.text(draft.name)
    if (changed.includes('enabled')) json.disabled = !draft.enabled
    if (changed.includes('intents')) json.allowed_intents = draft.intents
    if (Object.keys(json).length > 0) {
      emit('updated', await api(base, { method: 'PATCH', json, decode: decodeBotBot }))
      commit(changed.filter(k => k !== 'roles' && k !== 'avatar'))
    }
    if (changed.includes('roles')) {
      emit('updated', await api(`${base}/roles`, { method: 'PUT', json: { role_ids: draft.roles }, decode: decodeBotBot }))
      accessVersion.value++
      commit(['roles'])
    }
  },
  saved: () => { toasts.success(t('bots.saved')) }
})
const { draft, errors } = form
const avatar = useStagedImage({ get: () => draft.avatar, set: value => { draft.avatar = value }, savedUrl: () => props.bot.avatar_url ?? '' })

function setIntent(bit: number, on: boolean) {
  draft.intents = toggleIntent(draft.intents, bit, on)
}

defineExpose({ guard: form.guard })

async function remove() {
  const ok = await confirm({ title: t('bots.deleteTitle', { name: props.bot.name }), body: t('bots.deleteBody'), confirmLabel: t('common.delete'), danger: true })
  if (!ok) return
  try {
    await api(base, { method: 'DELETE' })
    emit('deleted', props.bot.id)
    toasts.success(t('bots.deleted'))
  } catch (e) {
    fail(e)
  }
}
</script>

<template>
  <div class="min-w-0 stack-6" :aria-label="$t('bots.detail', { name: bot.name })" role="group" data-testid="bot-detail" @keydown="form.onKeydown">
    <div class="flex flex-wrap items-center gap-3">
      <img v-if="avatar.previewUrl.value" :src="avatar.previewUrl.value" alt="" class="h-12 w-12 shrink-0 rounded-full object-cover" data-testid="bot-avatar-preview" />
      <span v-else class="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-mnema-raised text-lg font-semibold text-mnema-muted" aria-hidden="true">{{ botInitial(bot.name) }}</span>
      <label class="min-w-0 flex-1 stack-1">
        <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('common.name') }}</span>
        <input v-model="draft.name" v-max-chars="24" :aria-invalid="errors.name ? 'true' : undefined" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text outline-hidden transition focus:border-mnema-accent" data-testid="bot-name" @keydown.enter.prevent="form.save" />
        <span v-if="errors.name" class="block text-xs text-mnema-danger" data-testid="bot-name-error">{{ errors.name }}</span>
      </label>
      <label class="flex cursor-pointer items-center gap-1.5 rounded-md border border-mnema-border-strong px-3 py-1.5 text-sm text-mnema-text transition hover:bg-mnema-hover">
        <ImagePlus class="h-4 w-4" aria-hidden="true" />
        <span>{{ $t('bots.avatar') }}</span>
        <input type="file" accept="image/*" class="sr-only" :aria-label="$t('bots.avatarFor', { name: bot.name })" data-testid="bot-avatar" @change="avatar.pick" />
      </label>
    </div>
    <p v-if="draft.avatar" class="-mt-4 text-xs text-mnema-accent" data-testid="bot-avatar-pending">{{ $t('draft.imagePending') }}</p>
    <div v-if="bot.description || homepage" class="-mt-2 stack-1" data-testid="bot-about">
      <p v-if="bot.description" class="text-sm whitespace-pre-wrap wrap-break-word text-mnema-muted">{{ bot.description }}</p>
      <a v-if="homepage" :href="homepage" target="_blank" rel="noopener noreferrer nofollow" class="flex min-w-0 items-center gap-1 text-xs text-mnema-accent hover:underline" data-testid="bot-homepage">
        <ExternalLink class="h-3 w-3 shrink-0" aria-hidden="true" />
        <span class="truncate" :title="homepage">{{ homepage }}</span>
      </a>
    </div>

    <label class="flex items-center gap-2 text-sm text-mnema-text">
      <input v-model="draft.enabled" type="checkbox" data-testid="bot-enabled" />
      <span>{{ $t('bots.enabledLabel') }}</span>
    </label>
    <p v-if="!draft.enabled" class="-mt-4 text-xs text-mnema-tertiary">{{ $t('bots.disabledHint') }}</p>

    <fieldset class="stack-2">
      <legend class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('bots.intents.title') }}</legend>
      <p class="text-sm text-mnema-tertiary">{{ $t('bots.intents.hint') }}</p>
      <label v-for="intent in BOT_INTENTS" :key="intent.key" class="flex items-start gap-2 text-sm text-mnema-text">
        <input type="checkbox" class="mt-0.5" :checked="hasIntent(draft.intents, intent.bit)" :data-intent="intent.key" @change="setIntent(intent.bit, ($event.target as HTMLInputElement).checked)" />
        <span>
          <span class="block">{{ $t(`bots.intents.${intent.key}.name`) }}</span>
          <span class="block text-xs text-mnema-tertiary">{{ $t(`bots.intents.${intent.key}.desc`) }}</span>
        </span>
      </label>
    </fieldset>

    <BotRolesPanel v-model:roles="draft.roles" :bot="bot" />

    <UnsavedChangesBar sticky :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />

    <BotChannelAccessPanel :bot-id="bot.id" @saved="accessVersion++" />
    <BotAccessPanel :bot-id="bot.id" :version="accessVersion" />
    <BotTokensPanel :bot-id="bot.id" :bot-name="bot.name" />
    <BotCommandsPanel :bot-id="bot.id" />

    <div class="border-t border-mnema-hairline pt-4">
      <button type="button" class="flex items-center gap-1.5 text-sm text-mnema-tertiary transition hover:text-mnema-danger" data-testid="bot-delete" @click="remove">
        <Trash2 class="h-4 w-4" />
        <span>{{ $t('bots.delete') }}</span>
      </button>
    </div>
  </div>
</template>
