<script setup lang="ts">
// The calls of direct messages on top of the app: the cards of what rings for
// the member, the caller's strip, and the ringtone. The ringtone is one of the
// sound effects (audio settings: on/off per sound, volume, output device), never
// plays for a silent call (do not disturb, or already in a call), and stops the
// moment nothing rings any more.
import { computed, onBeforeUnmount, watch } from 'vue'
import { playSoundEffect } from '../lib/soundEffects'
import { useDmCallsStore } from '../stores/dmCalls'
import { useVoiceStore } from '../stores/voice'
import IncomingCallCard from './IncomingCallCard.vue'
import OutgoingCallCard from './OutgoingCallCard.vue'

const MAX_CARDS = 3
const RING_EVERY_MS = 3000

const calls = useDmCallsStore()
const voice = useVoiceStore()
const shown = computed(() => calls.incoming.slice(0, MAX_CARDS))
// The caller's strip belongs to the window that is in the Talk: another window of theirs cannot hang up for it.
const outgoing = computed(() => (calls.outgoing && voice.currentChannelId === calls.outgoing.talkId ? calls.outgoing : null))
const audible = computed(() => calls.incoming.some(c => !c.silent))

let ringer: ReturnType<typeof setInterval> | undefined

function stopRinging() {
  clearInterval(ringer)
  ringer = undefined
}

watch(audible, on => {
  stopRinging()
  if (!on) return
  playSoundEffect('call_ring')
  ringer = setInterval(() => playSoundEffect('call_ring'), RING_EVERY_MS)
}, { immediate: true })

onBeforeUnmount(stopRinging)
</script>

<template>
  <div
    v-if="shown.length || outgoing"
    class="pointer-events-none fixed inset-x-0 top-3 z-70 flex flex-col items-center gap-2 px-3"
    data-testid="call-host"
  >
    <IncomingCallCard v-for="call in shown" :key="call.conversationId" :call="call" />
    <OutgoingCallCard v-if="outgoing" :call="outgoing" />
  </div>
</template>
