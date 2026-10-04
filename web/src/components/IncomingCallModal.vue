<script setup>
import { onMounted, onUnmounted } from 'vue'
import { Phone, PhoneOff, Radio } from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'
import UserAvatar from './UserAvatar.vue'

const chatStore = useChatStore()
let ringtoneTimer = null

onMounted(() => {
  // Gentle periodic chime while ringing
  ringtoneTimer = setInterval(() => {
    if (chatStore.incomingCall) {
      try {
        const AudioCtx = window.AudioContext || window.webkitAudioContext
        if (!AudioCtx) return
        const ctx = new AudioCtx()
        const osc = ctx.createOscillator()
        const gain = ctx.createGain()
        osc.type = 'sine'
        osc.frequency.setValueAtTime(480, ctx.currentTime)
        osc.frequency.setValueAtTime(440, ctx.currentTime + 0.15)
        gain.gain.setValueAtTime(0.12, ctx.currentTime)
        gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + 0.8)
        osc.connect(gain)
        gain.connect(ctx.destination)
        osc.start()
        osc.stop(ctx.currentTime + 0.8)
      } catch (e) {
        // Audio policy
      }
    }
  }, 2500)
})

onUnmounted(() => {
  if (ringtoneTimer) {
    clearInterval(ringtoneTimer)
    ringtoneTimer = null
  }
})
</script>

<template>
  <div 
    v-if="chatStore.incomingCall" 
    class="fixed top-5 right-5 z-50 animate-bounce-subtle pointer-events-auto"
  >
    <div class="bg-mnema-elevated border-2 border-mnema-accent/60 rounded-2xl p-4 shadow-[0_10px_30px_rgba(0,0,0,0.8)] w-80 backdrop-blur-none flex flex-col gap-3">
      <div class="flex items-center gap-3">
        <div class="relative flex-shrink-0">
          <UserAvatar :user="chatStore.incomingCall.caller" size="lg" />
          <div class="absolute -bottom-1 -right-1 w-5 h-5 rounded-full bg-mnema-accent flex items-center justify-center text-mnema-canvas shadow-md animate-pulse">
            <Radio class="w-3 h-3 text-white" />
          </div>
        </div>

        <div class="flex-1 min-w-0">
          <h4 class="font-bold text-sm text-mnema-text truncate">
            {{ chatStore.incomingCall.caller?.display_name || chatStore.incomingCall.caller?.username }}
          </h4>
          <p class="text-xs text-mnema-accent font-medium flex items-center gap-1.5 animate-pulse">
            <span class="w-2 h-2 rounded-full bg-mnema-accent"></span>
            <span>Eingehender Server-Call...</span>
          </p>
          <span class="text-[10px] text-mnema-tertiary font-mono">Pion SFU Audio & Video</span>
        </div>
      </div>

      <div class="flex items-center gap-2 pt-1">
        <button
          @click="chatStore.acceptDMCall"
          class="flex-1 py-2 px-3 rounded-xl bg-mnema-accent hover:bg-mnema-accent-hover text-mnema-accent-ink font-bold text-xs flex items-center justify-center gap-2 transition shadow-md active:scale-95 cursor-pointer"
        >
          <Phone class="w-4 h-4" />
          <span>Annehmen</span>
        </button>

        <button
          @click="chatStore.rejectDMCall"
          class="flex-1 py-2 px-3 rounded-xl bg-mnema-danger/20 hover:bg-mnema-danger/30 text-mnema-danger border border-mnema-danger/40 font-bold text-xs flex items-center justify-center gap-2 transition shadow-md active:scale-95 cursor-pointer"
        >
          <PhoneOff class="w-4 h-4" />
          <span>Ablehnen</span>
        </button>
      </div>
    </div>
  </div>
</template>
