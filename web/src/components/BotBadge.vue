<script setup lang="ts">
// The small tag next to the name of a bot ("BOT") or of an incoming webhook
// ("WEBHOOK"). The one component used wherever such a name appears: messages,
// replies, threads, the member list, profiles, mentions and the audit log.
defineProps({
  // A webhook author, which keeps its own label.
  webhook: { type: Boolean, default: false }
})
</script>

<template>
  <span
    class="flex-shrink-0 rounded px-1 py-px text-[10px] font-semibold uppercase leading-[14px] tracking-wide bg-mnema-accent-subtle text-mnema-accent border border-mnema-accent/30"
    :data-testid="webhook ? 'webhook-badge' : 'bot-badge'"
  >{{ webhook ? $t('chat.webhook') : $t('chat.bot') }}</span>
</template>
