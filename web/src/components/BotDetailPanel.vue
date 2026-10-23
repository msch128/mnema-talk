<script setup lang="ts">
// One bot: rename, avatar, on/off, allowed intents, delete, plus the roles,
// access preview, tokens and commands sections.
import { ref, watch } from 'vue'
import { ImagePlus, Trash2 } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeBotBot } from '../types/rest'
import type { BotBot } from '../types/rest'
import { confirm } from '../lib/confirm'
import { BOT_INTENTS, botInitial, hasIntent, toggleIntent } from '../lib/bots'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BotRolesPanel from './BotRolesPanel.vue'
import BotAccessPanel from './BotAccessPanel.vue'
import BotTokensPanel from './BotTokensPanel.vue'
import BotCommandsPanel from './BotCommandsPanel.vue'

const props = defineProps({
  bot: { type: Object as () => BotBot, required: true }
})
const emit = defineEmits<{ updated: [bot: BotBot]; deleted: [id: string] }>()

const toasts = useToastStore()
const name = ref(props.bot.name)
const busy = ref(false)
// Bumped when roles change so the access preview reloads.
const accessVersion = ref(0)
const base = `/api/admin/bots/${props.bot.id}`

watch(() => props.bot.name, value => { name.value = value })

function fail(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}

async function patch(json: { name?: string; allowed_intents?: number; disabled?: boolean }) {
  if (busy.value) return
  busy.value = true
  try {
    emit('updated', await api(base, { method: 'PATCH', json, decode: decodeBotBot }))
  } catch (e) {
    fail(e)
    name.value = props.bot.name
  } finally {
    busy.value = false
  }
}

function rename() {
  const trimmed = name.value.trim()
  if (!trimmed) {
    toasts.error(t('bots.nameRequired'))
    name.value = props.bot.name
    return
  }
  if (trimmed !== props.bot.name) void patch({ name: trimmed })
}

async function setAvatar(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  const form = new FormData()
  form.append('avatar', file)
  try {
    emit('updated', await api(`${base}/avatar`, { method: 'POST', form, decode: decodeBotBot }))
  } catch (e) {
    fail(e)
  }
}

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

function onRoles(bot: BotBot) {
  accessVersion.value++
  emit('updated', bot)
}
</script>

<template>
  <div class="min-w-0 space-y-6" :aria-label="$t('bots.detail', { name: bot.name })" role="group" data-testid="bot-detail">
    <div class="flex flex-wrap items-center gap-3">
      <img v-if="bot.avatar_url" :src="bot.avatar_url" alt="" class="h-12 w-12 flex-shrink-0 rounded-full object-cover" />
      <span v-else class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-full bg-mnema-raised text-lg font-semibold text-mnema-muted" aria-hidden="true">{{ botInitial(bot.name) }}</span>
      <label class="min-w-0 flex-1 space-y-1">
        <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('common.name') }}</span>
        <input v-model="name" maxlength="24" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text outline-none transition focus:border-mnema-accent" data-testid="bot-name" @blur="rename" @keydown.enter.prevent="rename" />
      </label>
      <label class="flex cursor-pointer items-center gap-1.5 rounded-md border border-mnema-border-strong px-3 py-1.5 text-sm text-mnema-text transition hover:bg-mnema-hover">
        <ImagePlus class="h-4 w-4" aria-hidden="true" />
        <span>{{ $t('bots.avatar') }}</span>
        <input type="file" accept="image/*" class="sr-only" :aria-label="$t('bots.avatarFor', { name: bot.name })" data-testid="bot-avatar" @change="setAvatar" />
      </label>
    </div>

    <label class="flex items-center gap-2 text-sm text-mnema-text">
      <input type="checkbox" class="accent-mnema-accent" :checked="!bot.disabled" :disabled="busy" data-testid="bot-enabled" @change="patch({ disabled: !($event.target as HTMLInputElement).checked })" />
      <span>{{ $t('bots.enabledLabel') }}</span>
    </label>
    <p v-if="bot.disabled" class="-mt-4 text-xs text-mnema-tertiary">{{ $t('bots.disabledHint') }}</p>

    <fieldset class="space-y-2">
      <legend class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('bots.intents.title') }}</legend>
      <p class="text-sm text-mnema-tertiary">{{ $t('bots.intents.hint') }}</p>
      <label v-for="intent in BOT_INTENTS" :key="intent.key" class="flex items-start gap-2 text-sm text-mnema-text">
        <input type="checkbox" class="mt-0.5 accent-mnema-accent" :checked="hasIntent(bot.allowed_intents, intent.bit)" :disabled="busy" :data-intent="intent.key" @change="patch({ allowed_intents: toggleIntent(bot.allowed_intents, intent.bit, ($event.target as HTMLInputElement).checked) })" />
        <span>
          <span class="block">{{ $t(`bots.intents.${intent.key}.name`) }}</span>
          <span class="block text-xs text-mnema-tertiary">{{ $t(`bots.intents.${intent.key}.desc`) }}</span>
        </span>
      </label>
    </fieldset>

    <BotRolesPanel :bot="bot" @updated="onRoles" />
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
