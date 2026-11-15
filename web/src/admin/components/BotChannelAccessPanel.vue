<script setup lang="ts">
// Where one bot may work: the ticked channels only, or every channel, with the
// permissions it gets (doc/bots.md, "Give a bot access to every channel"). An
// action of its own with its own Save, like tokens: it changes the bot's own
// role and overwrites, not the bot's profile. The server decides every rule.
import { onMounted, ref } from 'vue'
import { api, caughtErrorMessage } from '../../shared/lib/api'
import { decodeBotAccess } from '../../types/rest'
import type { BotAccess } from '../../types/rest'
import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'
import { BOT_CHANNEL_PERMISSIONS, botPermissionKeys } from '../lib/bots'
import BotAccessChoice from './BotAccessChoice.vue'
import type { AccessMode } from './BotAccessChoice.vue'

const props = defineProps({
  botId: { type: String, required: true }
})
const emit = defineEmits<{ saved: [] }>()

const toasts = useToastStore()
const loaded = ref(false)
const busy = ref(false)
const error = ref('')
const mode = ref<AccessMode>('channels')
const permissions = ref<string[]>([])
const channels = ref<string[]>([])
const mixed = ref(false)
const privateReach = ref(false)

function adopt(access: BotAccess) {
  // A server older than this choice leaves the fields out.
  mode.value = access.mode ?? 'channels'
  permissions.value = botPermissionKeys(access.granted ?? 0)
  channels.value = [...(access.selected_channel_ids ?? [])]
  mixed.value = access.mixed ?? false
  privateReach.value = access.private_reach ?? false
}

async function load() {
  try {
    adopt(await api(`/api/admin/bots/${props.botId}/access`, { decode: decodeBotAccess }))
    loaded.value = true
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  }
}

async function save() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const json = { access: mode.value, permissions: permissions.value, channel_ids: channels.value }
    adopt(await api(`/api/admin/bots/${props.botId}/access`, { method: 'PUT', json, decode: decodeBotAccess }))
    toasts.success(t('bots.choice.saved'))
    emit('saved')
  } catch (e) {
    error.value = caughtErrorMessage(e, t('admin.unknownError'))
  } finally {
    busy.value = false
  }
}

function toggle(key: string, on: boolean) {
  permissions.value = on ? [...permissions.value, key] : permissions.value.filter(k => k !== key)
}

onMounted(load)
</script>

<template>
  <section v-if="loaded" class="min-w-0 stack-3" data-testid="bot-channel-access" :aria-busy="busy ? 'true' : 'false'">
    <h4 class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('bots.choice.sectionTitle') }}</h4>
    <p class="text-sm text-mnema-tertiary">{{ $t('bots.choice.settingsHint') }}</p>
    <p v-if="mixed" class="rounded-md border border-mnema-hairline bg-mnema-surface px-3 py-2 text-sm text-mnema-muted" data-testid="bot-channel-access-mixed">{{ $t('bots.choice.mixedNote') }}</p>
    <p v-if="mode === 'channels' && privateReach" class="rounded-md border border-mnema-hairline bg-mnema-surface px-3 py-2 text-sm text-mnema-muted" data-testid="bot-channel-access-private-reach">{{ $t('bots.choice.privateReach') }}</p>
    <fieldset class="stack-2">
      <legend class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('bots.choice.permissionsTitle') }}</legend>
      <label v-for="key in BOT_CHANNEL_PERMISSIONS" :key="key" class="flex items-start gap-2 text-sm text-mnema-text">
        <input type="checkbox" class="mt-0.5 shrink-0" :checked="permissions.includes(key)" :data-permission="key" @change="toggle(key, ($event.target as HTMLInputElement).checked)" />
        <span class="min-w-0">
          <span class="block">{{ $t(`perm.${key}.name`) }}</span>
          <span class="block text-xs text-mnema-tertiary">{{ $t(`perm.${key}.desc`) }}</span>
        </span>
      </label>
    </fieldset>
    <BotAccessChoice v-model:mode="mode" v-model:channels="channels" :disabled="busy" />
    <p v-if="error" role="alert" class="text-sm wrap-break-word text-mnema-danger" data-testid="bot-channel-access-error">{{ error }}</p>
    <div class="flex justify-end">
      <button type="button" class="btn btn-primary" :disabled="busy" data-testid="bot-channel-access-save" @click="save">{{ $t('bots.choice.save') }}</button>
    </div>
  </section>
</template>
