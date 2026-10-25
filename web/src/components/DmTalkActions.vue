<script setup lang="ts">
// A conversation has no call of its own. "Start a Talk" opens a temporary Talk
// from a Create-a-Talk lobby and posts an invitation into the conversation;
// "Invite to my Talk" posts the same card for the Talk the member is in.
import { computed, ref } from 'vue'
import { AudioLines, UserPlus } from '@lucide/vue'
import { isApiError, caughtErrorMessage } from '../lib/api'
import { inviteConversationToTalk, startConversationTalk } from '../lib/talks'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { useWebRTC } from '../composables/useWebRTC'
import { navigate } from '../lib/router'
import { t } from '../i18n'

const props = defineProps({
  conversationId: { type: String, required: true },
  canSend: { type: Boolean, default: false }
})

const chat = useChatStore()
const voice = useVoiceStore()
const permissions = usePermissionsStore()
const toasts = useToastStore()
const { joinVoiceChannel } = useWebRTC()
const busy = ref(false)

const myTalk = computed(() => chat.allChannels.find(c => c.id === voice.currentChannelId && c.type === 'voice' && !c.is_lobby) ?? null)

async function start() {
  if (busy.value) return
  busy.value = true
  try {
    const started = await startConversationTalk(props.conversationId)
    await Promise.all([chat.fetchChannels(), permissions.load()])
    const talk = chat.allChannels.find(c => c.id === started.channel_id)
    if (talk) void chat.selectChannel(talk)
    void joinVoiceChannel(started.channel_id)
    voice.activeView = 'voice'
    navigate(chat.linkTo(started.channel_id))
  } catch (e) {
    toasts.error(isApiError(e) && e.status === 409 ? t('dm.noTalkLobby') : caughtErrorMessage(e, t('dm.startTalkFailed')))
  } finally {
    busy.value = false
  }
}

async function invite() {
  const talk = myTalk.value
  if (busy.value || !talk) return
  busy.value = true
  try {
    await inviteConversationToTalk(talk.id, props.conversationId)
    toasts.success(t('dm.talkInvited'))
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('dm.inviteTalkFailed')))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <button
    type="button"
    data-testid="dm-start-talk"
    class="btn btn-icon w-9 h-9 max-md:w-11 max-md:h-11"
    :disabled="!canSend || busy"
    v-tooltip="$t('dm.startTalk')"
    :aria-label="$t('dm.startTalk')"
    @click="start"
  >
    <AudioLines class="w-5 h-5" />
  </button>
  <button
    v-if="myTalk"
    type="button"
    data-testid="dm-invite-to-talk"
    class="btn btn-icon w-9 h-9 max-md:w-11 max-md:h-11"
    :disabled="!canSend || busy"
    v-tooltip="$t('dm.inviteToTalk')"
    :aria-label="$t('dm.inviteToTalk')"
    @click="invite"
  >
    <UserPlus class="w-5 h-5" />
  </button>
</template>
