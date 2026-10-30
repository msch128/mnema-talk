<script setup lang="ts">
// A conversation has no call of its own. "Start a Talk" opens a temporary Talk
// that belongs to the conversation (no lobby needed) and rings its members;
// "Invite to my Talk" posts the same card for the Talk the member is in.
import { computed, ref } from 'vue'
import { Phone, PhoneCall, UserPlus } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { inviteConversationToTalk } from '../lib/talks'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useDmCallsStore } from '../stores/dmCalls'
import { useToastStore } from '../stores/toast'
import { useDmCall } from '../composables/useDmCall'
import { t } from '../i18n'

const props = defineProps({
  conversationId: { type: String, required: true },
  canSend: { type: Boolean, default: false }
})

const chat = useChatStore()
const voice = useVoiceStore()
const toasts = useToastStore()
const call = useDmCall()
const calls = useDmCallsStore()
const busy = ref(false)

const myTalk = computed(() => chat.allChannels.find(c => c.id === voice.currentChannelId && c.type === 'voice' && !c.is_lobby) ?? null)

// A call is going on here: joining it beats starting a second Talk, which would end its ring.
const running = computed(() => calls.active[props.conversationId] ?? null)
const inRunning = computed(() => !!running.value && voice.currentChannelId === running.value.talkId)

async function start() {
  if (busy.value) return
  busy.value = true
  try {
    if (running.value) await call.join(running.value.talkId)
    else await call.start(props.conversationId)
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
    v-if="!inRunning"
    type="button"
    :data-testid="running ? 'dm-join-talk' : 'dm-start-talk'"
    :class="['btn btn-icon w-9 h-9 max-md:w-11 max-md:h-11', running ? 'text-mnema-accent' : '']"
    :disabled="(!running && !canSend) || busy"
    v-tooltip="running ? $t('call.joinRunning') : $t('dm.startTalk')"
    :aria-label="running ? $t('call.joinRunning') : $t('dm.startTalk')"
    @click="start"
  >
    <PhoneCall v-if="running" class="w-5 h-5" />
    <Phone v-else class="w-5 h-5" />
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
