<script setup lang="ts">
// The components of a message, wherever a message is shown (timeline, thread
// root, thread replies). Only bots set components, so other messages show nothing.
import type { PropType } from 'vue'
import type { ThreadRoot } from '../../stores/chat'
import { shownName } from '../../shared/lib/deletedUser'
import { messageComponentsPath } from '../lib/components'
import MessageComponents from './MessageComponents.vue'

defineProps({
  msg: { type: Object as PropType<ThreadRoot>, required: true }
})
</script>

<template>
  <MessageComponents
    v-if="msg.components?.length && msg.channel_id"
    :components="msg.components"
    :endpoint="messageComponentsPath(msg.channel_id, msg.id)"
    :bot-name="shownName(msg)"
  />
</template>
