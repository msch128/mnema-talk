<script setup lang="ts">
import type { PropType } from 'vue'
import type { ChatDirectMessage } from '../types/rest'
// Mutes or unmutes the open conversation: no sound, no highlight, no push;
// unread still counts. It is the conversation's own setting, so it also
// overrides "Mute all direct messages".
import { computed } from 'vue'
import { Bell, BellOff } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useDmListActions } from '../composables/useDmListActions'

const props = defineProps({
  dm: { type: Object as PropType<ChatDirectMessage>, required: true }
})

const actions = useDmListActions()
const chat = useChatStore()
const muted = computed(() => chat.notificationLevel(props.dm.id) === 'mute')
</script>

<template>
  <button
    type="button"
    data-testid="dm-mute"
    class="btn btn-icon h-9 w-9 max-md:h-11 max-md:w-11"
    :aria-pressed="muted ? 'true' : 'false'"
    :aria-label="muted ? $t('dm.unmute') : $t('dm.mute')"
    v-tooltip="muted ? $t('dm.unmute') : $t('dm.mute')"
    @click="actions.toggleMute(props.dm)"
  >
    <BellOff v-if="muted" class="h-5 w-5" />
    <Bell v-else class="h-5 w-5" />
  </button>
</template>
