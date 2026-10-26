<script setup lang="ts">
// Settings of one Talk. Members who manage channels can rename it, make it a
// Create-a-Talk lobby and use the access presets; the leader of a temporary Talk
// can rename it and set its member limit (doc/talks.md).
import type { PropType } from 'vue'
import { computed, ref } from 'vue'
import { caughtErrorMessage } from '../lib/api'
import { setLobby, updateTalk, isTempTalk } from '../lib/talks'
import type { Channel } from '../types/domain'
import { usePermissionsStore } from '../stores/permissions'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import TalkAccessPresets from './TalkAccessPresets.vue'

type PresetsHandle = { save: () => Promise<void>; canSave: boolean }

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
const name = ref(props.channel.name)
const lobby = ref(props.channel.is_lobby === true)
const busy = ref(false)

const presets = ref<PresetsHandle | null>(null)
const nameChanged = computed(() => !!name.value.trim() && name.value.trim() !== props.channel.name)
const canSave = computed(() => !busy.value && (nameChanged.value || !!presets.value?.canSave))

// One Save for the whole form: the name and, when it changed, who may join.
async function save() {
  if (!canSave.value) return
  busy.value = true
  try {
    if (nameChanged.value) await updateTalk(props.channel.id, { name: name.value.trim() }, canManage.value)
    await presets.value?.save()
    await chat.fetchChannels()
    toasts.success(t('talk.presetApplied'))
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('sidebar.updateFailed')))
  } finally {
    busy.value = false
  }
}

async function toggleLobby() {
  const wanted = lobby.value
  busy.value = true
  try {
    await setLobby(props.channel.id, wanted)
    await chat.fetchChannels()
    toasts.success(t('talk.lobbySaved'))
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('sidebar.updateFailed')))
  } finally {
    busy.value = false
  }
  lobby.value = props.channel.is_lobby === true
}

const field = 'w-full rounded-md border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden'
</script>

<template>
  <BaseDialog :title="$t('talk.settingsTitle', { name: channel.name })" panel-class="max-w-md" @close="emit('close')">
    <div class="space-y-5 overflow-y-auto p-5" data-testid="talk-settings">
      <form class="space-y-1.5" @submit.prevent="save">
        <label for="talk-name" class="font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ $t('admin.channelNameLabel') }}</label>
        <input id="talk-name" v-model="name" type="text" maxlength="64" data-testid="talk-name" :class="field" />
      </form>

      <label v-if="canMakeLobby" class="flex items-start gap-3">
        <input v-model="lobby" type="checkbox" class="mt-1 accent-mnema-accent" data-testid="talk-lobby" :disabled="busy" @change="toggleLobby" />
        <span>
          <span class="block text-sm font-medium text-mnema-text">{{ $t('talk.lobby') }}</span>
          <span class="text-xs text-mnema-tertiary">{{ $t('talk.lobbyHint') }}</span>
        </span>
      </label>

      <TalkAccessPresets ref="presets" :channel="channel" :can-access="canManage && !channel.is_temp" />
      <p v-if="channel.is_temp && canManage" class="text-xs text-mnema-tertiary">{{ $t('talk.followsLobby') }}</p>
    </div>
    <div class="flex justify-end border-t border-mnema-hairline px-5 py-3">
      <button type="button" class="btn btn-primary h-9" data-testid="talk-save" :disabled="!canSave" @click="save">{{ $t('common.save') }}</button>
    </div>
  </BaseDialog>
</template>
