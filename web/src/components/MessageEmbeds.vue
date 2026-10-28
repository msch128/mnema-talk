<script setup lang="ts">
import type { PropType } from 'vue'
import type { ChatEmbed } from '../types/rest'
// Rich cards of a webhook message. Text is rendered like any message
// (Markdown, sanitized); pictures are never loaded from their host, they show
// as links the reader chooses to open.
import { ExternalLink } from '@lucide/vue'
import { locale } from '../i18n'
import { dateTime } from '../lib/time'
import MarkdownContent from './MarkdownContent.vue'
import TalkInviteCard from './TalkInviteCard.vue'

defineProps({
  embeds: { type: Array as PropType<ChatEmbed[]>, default: () => [] },
  /** Who wrote the message, for Talk invitations. */
  author: { type: String, default: '' }
})

function accent(color: number): string {
  return `#${color.toString(16).padStart(6, '0')}`
}

function host(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

function when(timestamp: string): string {
  return dateTime(timestamp, locale.value)
}
</script>

<template>
  <div v-if="embeds.length" class="mt-2 space-y-2" data-testid="embeds">
    <template v-for="(embed, i) in embeds" :key="i">
    <TalkInviteCard v-if="embed.talk_id" :talk-id="embed.talk_id" :title="embed.title ?? ''" :from="author" />
    <article
      v-else
      data-testid="embed"
      class="max-w-lg rounded-lg border border-mnema-border border-l-4 bg-mnema-elevated/70 p-3 text-left select-text"
      :style="embed.color === undefined ? undefined : { borderLeftColor: accent(embed.color) }"
    >
      <p v-if="embed.author" class="mb-1 text-xs font-medium text-mnema-muted" data-testid="embed-author">
        <a v-if="embed.author.url" :href="embed.author.url" target="_blank" rel="noopener noreferrer" class="hover:underline">{{ embed.author.name }}</a>
        <template v-else>{{ embed.author.name }}</template>
      </p>
      <h4 v-if="embed.title" class="font-semibold text-sm text-mnema-text" data-testid="embed-title">
        <a v-if="embed.url" :href="embed.url" target="_blank" rel="noopener noreferrer" class="text-mnema-accent hover:underline">{{ embed.title }}</a>
        <template v-else>{{ embed.title }}</template>
      </h4>
      <MarkdownContent v-if="embed.description" :content="embed.description" class="mt-1 text-sm" />
      <dl v-if="embed.fields?.length" class="mt-2 grid grid-cols-1 gap-x-4 gap-y-2 sm:grid-cols-3">
        <div v-for="(field, j) in embed.fields" :key="j" :class="field.inline ? '' : 'sm:col-span-3'" data-testid="embed-field">
          <dt class="text-xs font-semibold text-mnema-muted">{{ field.name }}</dt>
          <dd class="text-sm text-mnema-text whitespace-pre-wrap wrap-break-word">{{ field.value }}</dd>
        </div>
      </dl>
      <ul v-if="embed.image || embed.thumbnail" class="mt-2 space-y-1 text-xs">
        <li v-if="embed.image">
          <a :href="embed.image.url" target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1 text-mnema-accent hover:underline" data-testid="embed-image">
            <ExternalLink class="w-3 h-3" aria-hidden="true" />{{ $t('chat.embedImage') }} ({{ host(embed.image.url) }})
          </a>
        </li>
        <li v-if="embed.thumbnail">
          <a :href="embed.thumbnail.url" target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1 text-mnema-accent hover:underline" data-testid="embed-thumbnail">
            <ExternalLink class="w-3 h-3" aria-hidden="true" />{{ $t('chat.embedThumbnail') }} ({{ host(embed.thumbnail.url) }})
          </a>
        </li>
      </ul>
      <p v-if="embed.footer || embed.timestamp" class="mt-2 text-xs text-mnema-tertiary" data-testid="embed-footer">
        <span v-if="embed.footer">{{ embed.footer.text }}</span>
        <span v-if="embed.footer && embed.timestamp" aria-hidden="true"> · </span>
        <time v-if="embed.timestamp" :datetime="embed.timestamp">{{ when(embed.timestamp) }}</time>
      </p>
    </article>
    </template>
  </div>
</template>
