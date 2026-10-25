<script setup lang="ts">
// The invitation card of a Talk ("Start a Talk", "Invite to my Talk"). It is a
// link and nothing more: Join runs the ordinary join, so a member who may not
// see or connect to the Talk gets no button, and the server checks again.
import { computed } from 'vue'
import { Volume2 } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { usePermissionsStore } from '../stores/permissions'
import { useWebRTC } from '../composables/useWebRTC'
import { navigate } from '../lib/router'

const props = defineProps({
  talkId: { type: String, required: true },
  /** The Talk's name when the card was posted. */
  title: { type: String, default: '' },
  /** Who invites. */
  from: { type: String, default: '' }
})

const chat = useChatStore()
const voice = useVoiceStore()
const permissions = usePermissionsStore()
const { joinVoiceChannel } = useWebRTC()

const talk = computed(() => chat.allChannels.find(c => c.id === props.talkId && c.type === 'voice') ?? null)
const canJoin = computed(() => !!talk.value && permissions.canIn(props.talkId, 'connect'))
const inIt = computed(() => voice.currentChannelId === props.talkId)

function join() {
  if (!talk.value) return
  void chat.selectChannel(talk.value)
  void joinVoiceChannel(props.talkId)
  voice.activeView = 'voice'
  navigate(chat.linkTo(props.talkId))
}
</script>

<template>
  <div class="flex max-w-lg items-center gap-3 rounded-lg border border-mnema-border border-l-4 border-l-mnema-accent bg-mnema-elevated/70 p-3 text-left" data-testid="talk-invite">
    <Volume2 class="h-5 w-5 shrink-0 text-mnema-accent" aria-hidden="true" />
    <div class="min-w-0 flex-1">
      <p class="truncate text-sm font-semibold text-mnema-text">{{ talk?.name || title }}</p>
      <p class="text-xs text-mnema-tertiary">{{ from ? $t('talk.inviteFrom', { name: from }) : $t('talk.inviteGeneric') }}</p>
    </div>
    <span v-if="inIt" class="text-xs text-mnema-tertiary" data-testid="talk-invite-in">{{ $t('talk.inviteIn') }}</span>
    <button v-else-if="canJoin" type="button" class="btn btn-primary h-8 shrink-0" data-testid="talk-invite-join" @click="join">{{ $t('voice.join') }}</button>
    <span v-else class="text-xs text-mnema-tertiary" data-testid="talk-invite-unavailable">{{ $t('talk.inviteUnavailable') }}</span>
  </div>
</template>
