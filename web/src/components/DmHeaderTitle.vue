<script setup lang="ts">
import type { PropType } from 'vue'
import type { ChatDirectMessage } from '../types/rest'
// The left side of the header of a conversation: the face with the presence
// dot and the name. For a pair a click opens the member's profile; for a
// group, its members. A group shows how many people are in it and who.
import { computed } from 'vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { dmPeer, dmTitle } from '../lib/dm'
import { dmMemberNames } from '../lib/dmList'
import DmAvatar from './DmAvatar.vue'

const props = defineProps({
  dm: { type: Object as PropType<ChatDirectMessage>, required: true }
})
const emit = defineEmits<{ members: [] }>()

const authStore = useAuthStore()
const chatStore = useChatStore()

const me = computed(() => authStore.user?.id)
const title = computed(() => dmTitle(props.dm, me.value))
const peer = computed(() => dmPeer(props.dm, me.value))

function activate() {
  const person = peer.value
  if (!person) return emit('members')
  chatStore.openUserProfile({ ...person, role: chatStore.members.find(m => m.id === person.id)?.role ?? 'user' })
}
</script>

<template>
  <button
    type="button"
    data-testid="dm-header-title"
    class="flex min-w-0 items-center gap-2.5 rounded-md py-0.5 pr-2 text-left transition-colors hover:bg-mnema-hover/60 focus:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent max-md:min-h-11"
    :aria-label="peer ? $t('dm.openProfile', { name: title }) : $t('dm.info')"
    @click="activate"
  >
    <DmAvatar :dm="dm" ring-class="bg-mnema-canvas" />
    <span class="min-w-0">
      <span data-testid="chat-channel-name" class="block truncate text-base font-semibold leading-5 text-mnema-text">{{ title }}</span>
      <span v-if="dm.group" class="block truncate text-xs leading-4 text-mnema-tertiary" data-testid="dm-header-members">
        {{ $t('dm.memberCount', { n: dm.members.length }) }} · {{ dmMemberNames(dm, me) }}
      </span>
    </span>
  </button>
</template>
