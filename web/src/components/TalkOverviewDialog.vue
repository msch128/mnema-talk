<script setup lang="ts">
// Who is in a Talk, opened from a member's profile: the same list as the
// "N participants" popover of the Talk header, and Join. A Talk the viewer
// cannot see is not described at all, and one without the Connect permission
// shows its people but offers no Join.
import { computed } from 'vue'
import { Volume2 } from '@lucide/vue'
import { orderParticipants } from '../composables/useTalkStage'
import { useTalkJoin } from '../composables/useTalkJoin'
import { navigate } from '../lib/router'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useVoiceStore } from '../stores/voice'
import type { TalkParticipant } from './presentationTypes'
import BaseDialog from './BaseDialog.vue'
import TalkParticipantList from './TalkParticipantList.vue'

const props = defineProps({ channelId: { type: String, required: true } })
const emit = defineEmits<{ close: []; select: [user: TalkParticipant] }>()

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const permissions = usePermissionsStore()

// Only Talks in the viewer's own channel list: that is what they may see.
const talk = computed(() => chatStore.allChannels.find(c => c.id === props.channelId && c.type === 'voice') ?? null)
const users = computed<TalkParticipant[]>(() => (talk.value ? orderParticipants(Object.values(voiceStore.channelUsers[props.channelId] || {})) : []))
const canJoin = computed(() => !!talk.value && permissions.canIn(props.channelId, 'connect'))
const inIt = computed(() => voiceStore.isConnected && voiceStore.currentChannelId === props.channelId)

const { join: joinTalk } = useTalkJoin(() => props.channelId, () => {
  if (talk.value) void chatStore.selectChannel(talk.value)
  voiceStore.activeView = 'voice'
  navigate(chatStore.linkTo(props.channelId))
})
async function join() {
  if (await joinTalk()) emit('close')
}
</script>

<template>
  <BaseDialog
    :title="talk?.name || $t('profile.talk')"
    panel-class="max-w-sm"
    @close="emit('close')"
  >
    <div class="min-h-0 flex-1 overflow-y-auto p-3" data-testid="talk-overview">
      <TalkParticipantList v-if="talk" :users="users" :started-at="voiceStore.roomStartedAt[channelId] ?? ''" :leader-id="talk.leader_id ?? ''" @select="emit('select', $event)" />
      <p v-else class="px-3 py-4 text-sm text-mnema-tertiary" data-testid="talk-overview-hidden">{{ $t('profile.talkHidden') }}</p>
    </div>
    <footer class="flex items-center justify-end gap-3 border-t border-mnema-hairline bg-mnema-raised px-6 py-4">
      <span v-if="inIt" class="mr-auto text-sm text-mnema-tertiary" data-testid="talk-overview-in">{{ $t('talk.inviteIn') }}</span>
      <button v-else-if="canJoin" type="button" data-testid="talk-overview-join" class="btn btn-primary max-md:min-h-11" @click="join">
        <Volume2 class="h-4 w-4" aria-hidden="true" />
        <span>{{ $t('voice.join') }}</span>
      </button>
      <span v-else class="mr-auto text-sm text-mnema-tertiary" data-testid="talk-overview-cannot">{{ $t('profile.talkCannotJoin') }}</span>
      <button type="button" class="btn btn-ghost max-md:min-h-11" @click="emit('close')">{{ $t('common.close') }}</button>
    </footer>
  </BaseDialog>
</template>
