<script setup lang="ts">
// Host for openChannelInvite() from chat/lib/channelInvite: one dialog for the
// channel the member chose "Invite people" in. It looks the channel up again on
// every change, so it closes by itself when the channel is gone.
import { computed, watch } from 'vue'
import { channelInviteFor, closeChannelInvite } from '../lib/channelInvite'
import { useChatStore } from '../../stores/chat'
import ChannelInviteDialog from './ChannelInviteDialog.vue'

const chat = useChatStore()
const channel = computed(() => chat.allChannels.find(c => c.id === channelInviteFor.value) ?? null)

watch(channel, current => { if (!current && channelInviteFor.value) closeChannelInvite() })
</script>

<template>
  <ChannelInviteDialog v-if="channel" :key="channel.id" :channel="channel" @close="closeChannelInvite" />
</template>
