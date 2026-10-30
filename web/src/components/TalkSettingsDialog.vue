<script setup lang="ts">
// Settings of one Talk. Members who manage channels can rename it, make it a
// Create-a-Talk lobby, choose who may join and set its member limit; the leader
// of a temporary Talk can rename it and set its member limit (doc/talks.md).
// Every field is part of one draft: nothing is stored until Save (doc/settings-ux.md).
import type { PropType } from 'vue'
import { computed, onMounted, ref } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import { updateTalk, isTempTalk } from '../lib/talks'
import type { TalkChange } from '../lib/talks'
import { currentPreset, presetEveryone, presetOnlyRoles, rolesLetIn } from '../lib/talkPresets'
import type { AccessPreset } from '../lib/talkPresets'
import { decodeOverwritesBody } from '../types/domain'
import type { Channel, Overwrite } from '../types/domain'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import { usePermissionsStore } from '../stores/permissions'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import TalkAccessPresets from './TalkAccessPresets.vue'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

interface TalkForm {
  name: string
  lobby: boolean
  limit: number | string
  access: AccessPreset
  roles: string[]
}

const props = defineProps({
  channel: { type: Object as PropType<Channel>, required: true }
})
const emit = defineEmits<{ close: [] }>()

const permissions = usePermissionsStore()
const features = useServerFeaturesStore()
const chat = useChatStore()
const toasts = useToastStore()

const canManage = computed(() => permissions.can('manage_channels'))
const canMakeLobby = computed(() => canManage.value && features.talksLobby && !isTempTalk(props.channel))
const canAccess = computed(() => canManage.value && !props.channel.is_temp)

// Who may join is stored as the Talk's overwrites; they are read once and written back on Save.
const overwrites = ref<Overwrite[]>([])
const loaded = ref(!canAccess.value)
const loadFailed = ref(false)
const everyoneId = computed(() => permissions.roles.find(r => r.is_everyone)?.id ?? '')
const overwritesPath = computed(() => `/api/admin/channels/${props.channel.id}/overwrites`)

async function load() {
  if (!canAccess.value) return
  loaded.value = false
  loadFailed.value = false
  try {
    overwrites.value = (await api(overwritesPath.value, { decode: decodeOverwritesBody })).overwrites
  } catch (e) {
    loadFailed.value = true
    toasts.error(caughtErrorMessage(e, t('overwrites.loadFailed')))
  } finally {
    loaded.value = true
  }
}
onMounted(load)

const form = useSettingsDraft<TalkForm>({
  source: () => ({
    name: props.channel.name,
    lobby: props.channel.is_lobby === true,
    limit: props.channel.user_limit || 0,
    access: canAccess.value ? currentPreset(overwrites.value, everyoneId.value) : 'everyone',
    roles: canAccess.value ? rolesLetIn(overwrites.value, everyoneId.value) : []
  }),
  normalisers: {
    name: normalise.text,
    lobby: normalise.flag,
    limit: normalise.count,
    // The roles only count while "Only these roles" is chosen.
    roles: (value, state) => (state.access === 'roles' ? normalise.set(value) : [])
  },
  validate: draft => {
    const found: Partial<Record<keyof TalkForm, string>> = {}
    if (!normalise.text(draft.name)) found.name = t('talk.nameRequired')
    const limit = Number(draft.limit)
    if (draft.limit === '' ? false : !Number.isInteger(limit) || limit < 0 || limit > 999) found.limit = t('talk.limitInvalid')
    if (draft.access === 'roles' && draft.roles.length === 0) found.roles = t('talk.rolesRequired')
    return found
  },
  blocked: () => !loaded.value || loadFailed.value,
  // Defined order: the Talk itself (name, limit, lobby) in one call, then who may join.
  save: async ({ changed, draft, commit }) => {
    const change: TalkChange = {}
    if (changed.includes('name')) change.name = normalise.text(draft.name)
    if (changed.includes('limit')) change.user_limit = Math.min(999, normalise.count(draft.limit))
    if (changed.includes('lobby')) change.is_lobby = draft.lobby
    if (Object.keys(change).length > 0) {
      await updateTalk(props.channel.id, change, canManage.value)
      commit(changed.filter(k => k === 'name' || k === 'limit' || k === 'lobby'))
    }
    if (canAccess.value && (changed.includes('access') || changed.includes('roles'))) {
      const next = draft.access === 'roles'
        ? presetOnlyRoles(overwrites.value, everyoneId.value, draft.roles)
        : presetEveryone(overwrites.value, everyoneId.value)
      await api(overwritesPath.value, { method: 'PUT', json: { overwrites: next } })
      overwrites.value = next
      commit(['access', 'roles'])
      await permissions.load()
    }
  },
  saved: async () => {
    await chat.fetchChannels()
    toasts.success(t('talk.presetApplied'))
  }
})
const { draft, errors } = form

const field = 'w-full rounded-md border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden'
</script>

<template>
  <BaseDialog :title="$t('talk.settingsTitle', { name: channel.name })" panel-class="max-w-lg" :guard="form.guard" @close="emit('close')">
    <form class="flex min-h-0 flex-1 flex-col" data-testid="talk-settings" @submit.prevent="form.save" @keydown="form.onKeydown">
      <div class="min-h-0 flex-1 space-y-6 overflow-y-auto p-6">
        <div class="space-y-1.5">
          <label for="talk-name" class="font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ $t('admin.channelNameLabel') }}</label>
          <input id="talk-name" v-model="draft.name" type="text" maxlength="64" data-testid="talk-name" :aria-invalid="errors.name ? 'true' : undefined" :class="field" />
          <p v-if="errors.name" class="text-xs text-mnema-danger" data-testid="talk-name-error">{{ errors.name }}</p>
        </div>

        <label v-if="canMakeLobby" class="flex items-start gap-3">
          <input v-model="draft.lobby" type="checkbox" class="mt-1 accent-mnema-accent" data-testid="talk-lobby" />
          <span>
            <span class="block text-sm font-medium text-mnema-text">{{ $t('talk.lobby') }}</span>
            <span class="text-xs text-mnema-tertiary">{{ $t('talk.lobbyHint') }}</span>
          </span>
        </label>

        <TalkAccessPresets
          v-model:access="draft.access"
          v-model:roles="draft.roles"
          v-model:limit="draft.limit"
          :can-access="canAccess"
          :locked="!loaded || loadFailed"
          :roles-error="errors.roles ?? ''"
          :limit-error="errors.limit ?? ''"
        />
        <button v-if="loadFailed" type="button" class="btn btn-secondary btn-sm" data-testid="preset-retry" @click="load">{{ $t('common.retry') }}</button>
        <p v-if="channel.is_temp && canManage" class="text-xs text-mnema-tertiary">{{ $t('talk.followsLobby') }}</p>
      </div>
      <UnsavedChangesBar :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
    </form>
  </BaseDialog>
</template>
