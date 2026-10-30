<script setup lang="ts">
// The record an unanswered call leaves in a conversation: a small line, not a
// message. The caller reads that nobody answered, the others who called them
// and a button to call back.
import { computed, ref } from 'vue'
import { PhoneCall, PhoneMissed } from '@lucide/vue'
import { useDmCall } from '../composables/useDmCall'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'

const props = defineProps({
  /** Who called (the author of the record). */
  from: { type: String, default: '' },
  fromId: { type: String, default: '' },
  conversationId: { type: String, default: '' }
})

const auth = useAuthStore()
const chat = useChatStore()
const call = useDmCall()
const busy = ref(false)

const mine = computed(() => !!props.fromId && props.fromId === auth.user?.id)
const canCallBack = computed(() => !mine.value && !!props.conversationId && chat.dms.some(d => d.id === props.conversationId && d.can_send))

async function callBack() {
  if (busy.value) return
  busy.value = true
  try {
    await call.start(props.conversationId)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="flex max-w-lg items-center gap-3 rounded-lg border border-mnema-border bg-mnema-elevated/50 px-3 py-2 text-left" data-testid="call-missed">
    <PhoneMissed :class="['h-4 w-4 shrink-0', mine ? 'text-mnema-tertiary' : 'text-mnema-danger']" aria-hidden="true" />
    <p class="min-w-0 flex-1 text-sm text-mnema-muted" data-testid="call-missed-text">
      {{ mine ? $t('call.missedMine') : $t('call.missedFrom', { name: from }) }}
    </p>
    <button
      v-if="canCallBack"
      type="button"
      class="btn btn-secondary btn-sm shrink-0"
      data-testid="call-missed-back"
      :disabled="busy"
      @click="callBack"
    >
      <PhoneCall class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('call.callBack') }}
    </button>
  </div>
</template>
