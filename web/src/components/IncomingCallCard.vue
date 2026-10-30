<script setup lang="ts">
import type { PropType } from 'vue'
// An incoming call, wherever the member is in the app: who calls, and the two
// things they can do about it. Join is the ordinary join of the call's Talk;
// Dismiss only closes the card here (the call goes on and its invitation stays
// in the conversation). The bar underneath runs out with the ring.
import { computed } from 'vue'
import { MessageSquare, PhoneIncoming, X } from '@lucide/vue'
import { useCallParty } from '../composables/useCallParty'
import { useDmCall } from '../composables/useDmCall'
import { useChatStore } from '../stores/chat'
import { useDmCallsStore, type IncomingCall } from '../stores/dmCalls'
import { t } from '../i18n'
import UserAvatar from './UserAvatar.vue'

const props = defineProps({
  call: { type: Object as PropType<IncomingCall>, required: true }
})

const chat = useChatStore()
const calls = useDmCallsStore()
const dmCall = useDmCall()
const { caller, callerName, title, group } = useCallParty(computed(() => props.call))

const remainingMs = computed(() => Math.max(0, props.call.until - Date.now()))
const context = computed(() => (group.value ? t('call.inGroup', { name: title.value }) : t('call.directMessage')))

function join() {
  void dmCall.join(props.call.talkId)
}

function message() {
  void chat.openDm(props.call.conversationId)
}
</script>

<template>
  <article
    class="pop-in pointer-events-auto relative w-full max-w-[26rem] overflow-hidden rounded-xl bg-mnema-elevated shadow-popover"
    role="alert"
    :aria-label="$t('call.incomingLabel', { name: callerName })"
    data-testid="incoming-call"
    :data-silent="call.silent ? 'true' : undefined"
  >
    <div class="flex items-center gap-3.5 px-4 pt-3.5">
      <span class="call-pulse relative inline-flex shrink-0 rounded-full">
        <UserAvatar :user="caller" size="lg" ring-class="bg-mnema-elevated" />
        <span class="absolute -bottom-1 -right-1 flex h-5 w-5 items-center justify-center rounded-full bg-mnema-accent text-mnema-accent-ink ring-2 ring-mnema-elevated" aria-hidden="true">
          <PhoneIncoming class="h-3 w-3" />
        </span>
      </span>
      <div class="min-w-0 flex-1">
        <p class="truncate text-base font-semibold text-mnema-text" data-testid="incoming-call-name">{{ callerName }}</p>
        <p class="text-sm font-medium text-mnema-accent">{{ $t('call.isCalling') }}</p>
        <p class="truncate text-xs text-mnema-tertiary" data-testid="incoming-call-context">{{ context }}</p>
      </div>
    </div>
    <div class="flex items-center gap-2 px-4 pb-3.5 pt-3">
      <button type="button" class="btn btn-primary h-9 min-w-0 flex-1 max-md:h-11" data-testid="incoming-call-join" @click="join">
        {{ $t('voice.join') }}
      </button>
      <button
        type="button"
        class="btn btn-secondary btn-icon h-9 w-9 shrink-0 max-md:h-11 max-md:w-11"
        data-testid="incoming-call-message"
        :aria-label="$t('call.openChat')"
        v-tooltip="$t('call.openChat')"
        @click="message"
      >
        <MessageSquare class="h-4 w-4" />
      </button>
      <button type="button" class="btn btn-ghost h-9 shrink-0 max-md:h-11" data-testid="incoming-call-dismiss" @click="calls.dismiss(call.conversationId)">
        <X class="h-4 w-4" aria-hidden="true" />{{ $t('call.dismiss') }}
      </button>
    </div>
    <div class="call-timer" :style="{ animationDuration: `${remainingMs}ms` }" aria-hidden="true"></div>
  </article>
</template>
