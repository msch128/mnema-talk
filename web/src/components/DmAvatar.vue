<script setup lang="ts">
import type { PropType } from 'vue'
import type { ChatDirectMessage } from '../types/rest'
// The face of a conversation: the other member with their presence dot, or
// two of a group's members overlapped.
import { computed } from 'vue'
import { Users } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { dmPeer, dmPeers } from '../lib/dm'
import UserAvatar from './UserAvatar.vue'

const props = defineProps({
  dm: { type: Object as PropType<ChatDirectMessage>, required: true },
  ringClass: { type: String, default: 'bg-mnema-raised' }
})

const authStore = useAuthStore()
const chatStore = useChatStore()

const me = computed(() => authStore.user?.id)
const peer = computed(() => dmPeer(props.dm, me.value))
const stack = computed(() => dmPeers(props.dm, me.value).slice(0, 2))
</script>

<template>
  <UserAvatar
    v-if="peer"
    :user="peer"
    size="sm"
    show-status
    :status="chatStore.presenceOf(peer.id)"
    :ring-class="ringClass"
  />
  <span v-else-if="stack.length > 1" class="relative inline-block h-8 w-8 shrink-0" data-testid="dm-avatar-stack" aria-hidden="true">
    <span
      v-for="(person, i) in stack"
      :key="person.id"
      :class="i === 0 ? 'absolute left-0 top-0 origin-top-left scale-[0.8]' : 'absolute bottom-0 right-0 origin-bottom-right scale-[0.8] rounded-full ring-2 ring-mnema-raised'"
    ><UserAvatar :user="person" size="xs" /></span>
  </span>
  <span v-else class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-mnema-surface text-mnema-tertiary" aria-hidden="true">
    <Users class="h-4 w-4" />
  </span>
</template>
