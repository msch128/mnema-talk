<script setup lang="ts">
import type { PropType } from 'vue'
// A phone beside a conversation that has a call: ringing for the member (it
// moves), the member calling (it pulses), or a call going on (it stands still).
import { computed } from 'vue'
import { Phone, PhoneIncoming, PhoneOutgoing } from '@lucide/vue'
import type { CallMark } from '../stores/dmCalls'

const props = defineProps({
  mark: { type: String as PropType<CallMark>, required: true }
})

const icon = computed(() => (props.mark === 'ringing' ? PhoneIncoming : props.mark === 'calling' ? PhoneOutgoing : Phone))
</script>

<template>
  <span
    :class="['inline-flex h-4 w-4 shrink-0 items-center justify-center text-mnema-accent', mark === 'ringing' ? 'call-phone-ring' : '']"
    :data-mark="mark"
    data-testid="call-mark"
  ><component :is="icon" class="h-3.5 w-3.5" aria-hidden="true" /></span>
</template>
