<script setup lang="ts">
// Admin dashboard: server-wide outgoing webhooks (Owner only). Webhooks of a
// single channel live in that channel's context menu.
import OutgoingWebhooksPanel from './OutgoingWebhooksPanel.vue'
</script>

<template>
  <OutgoingWebhooksPanel :channel-id="null" />
</template>
