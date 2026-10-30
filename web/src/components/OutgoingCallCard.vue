<script setup lang="ts">
import type { PropType } from 'vue'
// The caller's side: who is being called, until they join or the ring ends;
// then a plain "has not joined" with what can still be done. Cancelling is
// leaving the Talk, which is also what tells the others the call is over.
import { computed } from 'vue'
import { MessageSquare, PhoneOff, PhoneOutgoing } from '@lucide/vue'
import { useCallParty } from '../composables/useCallParty'
import { useDmCall } from '../composables/useDmCall'
import { useChatStore } from '../stores/chat'
import type { OutgoingCall } from '../stores/dmCalls'
import UserAvatar from './UserAvatar.vue'

const props = defineProps({
  call: { type: Object as PropType<OutgoingCall>, required: true }
})

const chat = useChatStore()
const dmCall = useDmCall()
const { peers, title, group } = useCallParty(computed(() => props.call))

const calling = computed(() => props.call.phase === 'calling')
const remainingMs = computed(() => Math.max(0, props.call.until - Date.now()))
</script>

<template>
  <article
    class="pop-in pointer-events-auto relative w-full max-w-[26rem] overflow-hidden rounded-xl bg-mnema-elevated shadow-popover"
    role="status"
    data-testid="outgoing-call"
    :data-phase="call.phase"
  >
    <div class="flex items-center gap-3.5 px-4 py-3">
      <span :class="['relative inline-flex shrink-0 rounded-full', calling ? 'call-pulse' : '']">
        <UserAvatar :user="peers[0] ?? null" size="md" ring-class="bg-mnema-elevated" />
        <span class="absolute -bottom-1 -right-1 flex h-4.5 w-4.5 items-center justify-center rounded-full bg-mnema-surface text-mnema-accent ring-2 ring-mnema-elevated" aria-hidden="true">
          <PhoneOutgoing class="h-2.5 w-2.5" />
        </span>
      </span>
      <div class="min-w-0 flex-1">
        <p class="text-sm font-semibold text-mnema-text wrap-break-word" data-testid="outgoing-call-text">
          {{ calling ? $t('call.calling', { name: title }) : (group ? $t('call.nobodyJoined') : $t('call.hasNotJoined', { name: title })) }}
        </p>
        <p class="text-xs text-mnema-tertiary">{{ calling ? $t('call.callingHint') : $t('call.waitingHint') }}</p>
      </div>
      <button
        v-if="!calling"
        type="button"
        class="btn btn-secondary btn-icon h-9 w-9 shrink-0 max-md:h-11 max-md:w-11"
        data-testid="outgoing-call-message"
        :aria-label="$t('call.openChat')"
        v-tooltip="$t('call.openChat')"
        @click="chat.openDm(call.conversationId)"
      >
        <MessageSquare class="h-4 w-4" />
      </button>
      <button type="button" class="btn btn-secondary h-9 shrink-0 max-md:h-11" data-testid="outgoing-call-cancel" @click="dmCall.hangUp()">
        <PhoneOff class="h-4 w-4" aria-hidden="true" />{{ calling ? $t('common.cancel') : $t('call.hangUp') }}
      </button>
    </div>
    <div v-if="calling" class="call-timer" :style="{ animationDuration: `${remainingMs}ms` }" aria-hidden="true"></div>
  </article>
</template>
