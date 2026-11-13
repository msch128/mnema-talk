<script setup lang="ts">
// The channels a bot added from a manifest may work in, grouped by category.
// None is ticked at first: the installer chooses every one.
import { computed } from 'vue'
import { Hash, MessagesSquare } from '@lucide/vue'
import { useChatStore } from '../../stores/chat'
import { installableChannels } from '../lib/manifest'

const selected = defineModel<string[]>({ required: true })
const chat = useChatStore()
const groups = computed(() => installableChannels(chat.categories, chat.uncategorized))

function toggle(id: string, on: boolean) {
  selected.value = on ? [...selected.value, id] : selected.value.filter(x => x !== id)
}
</script>

<template>
  <div class="stack-3" data-testid="manifest-channels">
    <p v-if="!groups.length" class="text-sm text-mnema-tertiary">{{ $t('bots.manifest.noChannels') }}</p>
    <fieldset v-for="group in groups" :key="group.id" class="stack-1">
      <legend class="mb-1 max-w-full truncate text-xs font-semibold uppercase tracking-wide text-mnema-tertiary" :title="group.name || undefined">{{ group.name || $t('bots.manifest.noCategory') }}</legend>
      <label v-for="channel in group.channels" :key="channel.id" class="flex min-w-0 items-center gap-2 rounded-md px-1 py-1 text-sm text-mnema-text hover:bg-mnema-hover">
        <input type="checkbox" class="shrink-0" :checked="selected.includes(channel.id)" :data-channel="channel.id" @change="toggle(channel.id, ($event.target as HTMLInputElement).checked)" />
        <MessagesSquare v-if="channel.type === 'forum'" class="h-3.5 w-3.5 shrink-0 text-mnema-tertiary" aria-hidden="true" />
        <Hash v-else class="h-3.5 w-3.5 shrink-0 text-mnema-tertiary" aria-hidden="true" />
        <span class="min-w-0 truncate" :title="channel.name">{{ channel.name }}</span>
      </label>
    </fieldset>
  </div>
</template>
