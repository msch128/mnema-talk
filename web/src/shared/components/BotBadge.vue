<script setup lang="ts">
// The small tag next to the name of a bot ("BOT") or of an incoming webhook
// ("WEBHOOK"). The one component used wherever such a name appears: messages,
// replies, threads, the member list, profiles, mentions and the audit log.
// Light text on the dark green band: it keeps AA contrast at 10 px even where
// its row is dimmed (the offline members of the member list are at 75 %).
defineProps({
  // A webhook author, which keeps its own label.
  webhook: { type: Boolean, default: false }
})
</script>

<template>
  <span
    class="shrink-0 rounded-sm px-1 py-px text-[10px] font-bold uppercase leading-[14px] tracking-wide bg-mnema-band text-mnema-text border border-mnema-mint/30"
    :data-testid="webhook ? 'webhook-badge' : 'bot-badge'"
  >{{ webhook ? $t('chat.webhook') : $t('chat.bot') }}</span>
</template>
