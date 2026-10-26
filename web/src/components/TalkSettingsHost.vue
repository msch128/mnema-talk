<script setup lang="ts">
// Host for openTalkSettings() from lib/talks: one dialog for the Talk whose
// settings were asked for. It looks the Talk up again on every change, so it
// closes by itself when the Talk is gone (a temporary Talk that emptied).
import { computed, watch } from 'vue'
import { closeTalkSettings, talkSettingsFor } from '../lib/talks'
import { useChatStore } from '../stores/chat'
import TalkSettingsDialog from './TalkSettingsDialog.vue'

const chat = useChatStore()
const channel = computed(() => chat.allChannels.find(c => c.id === talkSettingsFor.value) ?? null)

watch(channel, current => { if (!current && talkSettingsFor.value) closeTalkSettings() })
</script>

<template>
  <TalkSettingsDialog v-if="channel" :key="channel.id" :channel="channel" @close="closeTalkSettings" />
</template>
