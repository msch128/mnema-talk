<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { EyeOff, X } from '@lucide/vue'
import UserAvatar from './UserAvatar.vue'
import MarkdownContent from './MarkdownContent.vue'
import { useChatStore } from '../stores/chat'

// Replies of bots to slash commands that only this member sees ("ephemeral").
// The role=status wrapper stays in the page, so a new private reply is announced.
// They live in memory, below the messages of their channel, until dismissed.
const props = defineProps({ channelId: { type: String, required: true } })
const chat = useChatStore()
const notes = computed(() => chat.ephemeral.filter(n => n.channel_id === props.channelId))
const root = ref<HTMLElement | null>(null)

// The dismiss button leaves with its note: focus goes on to the next note, or
// back to the composer, instead of falling to the page.
async function dismiss(id: number) {
  const index = notes.value.findIndex(n => n.id === id)
  chat.dismissEphemeral(id)
  await nextTick()
  const next = root.value?.querySelectorAll<HTMLElement>('[data-ephemeral-dismiss]')[Math.min(index, notes.value.length - 1)]
  const target = next ?? document.querySelector<HTMLElement>('[data-composer-input]')
  target?.focus()
}
</script>

<template>
  <div ref="root" role="status">
  <div v-if="notes.length" class="flex flex-col gap-2 px-4 pb-2" data-testid="ephemeral-notes">
    <article
      v-for="n in notes"
      :key="n.id"
      class="flex gap-3 rounded-lg border border-dashed border-mnema-border bg-mnema-raised px-3 py-2 text-sm"
      :aria-label="$t('slash.privateFrom', { bot: n.bot_name })"
    >
      <UserAvatar :user="{ display_name: n.bot_name, username: n.bot_name, avatar_url: n.bot_avatar_url }" size="sm" />
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2">
          <span class="font-semibold text-mnema-text">{{ n.bot_name }}</span>
          <span class="rounded-sm bg-mnema-band px-1 text-[10px] font-bold uppercase tracking-wide text-mnema-mint">{{ $t('slash.bot') }}</span>
          <span class="flex items-center gap-1 text-xs text-mnema-tertiary"><EyeOff class="h-3.5 w-3.5" />{{ $t('slash.onlyYou') }}</span>
        </div>
        <MarkdownContent v-if="n.content" :content="n.content" :previews="false" class="mt-0.5" />
        <div v-for="(e, i) in n.embeds ?? []" :key="i" class="mt-1 border-l-2 border-mnema-border pl-2">
          <div v-if="e.title" class="font-semibold text-mnema-text">{{ e.title }}</div>
          <div v-if="e.description" class="whitespace-pre-wrap wrap-break-word text-mnema-muted">{{ e.description }}</div>
        </div>
      </div>
      <button
        type="button"
        class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-mnema-tertiary transition hover:bg-mnema-surface hover:text-mnema-text"
        data-ephemeral-dismiss
        :aria-label="$t('slash.dismiss')"
        @click="dismiss(n.id)"
      >
        <X class="h-4 w-4" />
      </button>
    </article>
  </div>
  </div>
</template>
