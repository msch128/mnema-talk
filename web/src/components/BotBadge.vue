<script setup lang="ts">
// Marks a message posted by a webhook, so it never passes for a member.
</script>

<template>
  <span
    class="flex-shrink-0 rounded px-1 py-px text-[10px] font-semibold uppercase tracking-wide bg-mnema-accent-subtle text-mnema-accent border border-mnema-accent/30"
    data-testid="bot-badge"
  >{{ $t('chat.bot') }}</span>
</template>
