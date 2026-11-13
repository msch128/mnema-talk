<script setup lang="ts">
// The buttons and select menus of a bot's message or private note, in their
// action rows (doc/bots.md, Message components). A click goes to the server,
// which hands it to the bot; the bot's answer comes back as an event (an edited
// message, a note or a form). Link buttons open their address after the usual
// question where a link leads. Anything this app does not know (a newer
// component type or button style) is left out with one quiet line.
import type { PropType } from 'vue'
import { computed } from 'vue'
import { ExternalLink } from '@lucide/vue'
import { actionRows, buttonText, hostOf, isSelect, linkUrl, LINK_STYLE, type ActionRow, type Component } from '../lib/components'
import { useComponentUse } from '../composables/useComponentUse'
import { openConfirmedUrl } from '../../shared/lib/markdownActions'
import ComponentSelect from './ComponentSelect.vue'

const props = defineProps({
  components: { type: Array as PropType<readonly ActionRow[] | null>, default: null },
  /** The route a use is posted to (a message's or a note's components). */
  endpoint: { type: String, required: true },
  botName: { type: String, default: '' }
})

const read = computed(() => actionRows(props.components))
const rows = computed(() => read.value.rows)
const { pending, error, use } = useComponentUse(() => props.endpoint, () => props.botName)

// Calm by default: only primary takes the accent; success and danger are tinted, never solid.
const STYLES: Record<number, string> = {
  1: 'btn-primary',
  2: 'btn-secondary',
  3: 'border-mnema-mint/40 bg-mnema-mint/10 text-mnema-mint hover:bg-mnema-mint/20',
  4: 'btn-danger',
  5: 'btn-secondary'
}

function press(c: Component) {
  const url = linkUrl(c)
  if (c.style === LINK_STYLE) {
    if (url) void openConfirmedUrl(url, hostOf(url))
    return
  }
  if (c.custom_id) void use(c.custom_id)
}

function choose(c: Component, values: string[]) {
  if (c.custom_id) void use(c.custom_id, values)
}

const isDisabled = (c: Component) => !!c.disabled || (c.style === LINK_STYLE ? !linkUrl(c) : !c.custom_id)
</script>

<template>
  <div v-if="rows.length || read.skipped" class="mt-2 flex min-w-0 flex-col gap-2" data-testid="message-components">
    <div v-for="(row, r) in rows" :key="r" role="group" class="flex min-w-0 flex-wrap items-center gap-2" data-testid="component-row">
      <template v-for="(c, i) in row" :key="c.custom_id || `${r}-${i}`">
        <ComponentSelect
          v-if="isSelect(c)"
          :component="c"
          :pending="pending === c.custom_id"
          :disabled="!!c.disabled || !c.custom_id || (pending !== null && pending !== c.custom_id)"
          @choose="choose(c, $event)"
        />
        <button
          v-else
          type="button"
          :class="['btn max-w-full whitespace-normal wrap-break-word text-left', STYLES[c.style ?? 2] ?? 'btn-secondary']"
          :disabled="isDisabled(c)"
          :aria-busy="pending === c.custom_id ? 'true' : undefined"
          :aria-label="c.style === LINK_STYLE && linkUrl(c) ? $t('components.opensLink', { label: buttonText(c), host: hostOf(linkUrl(c)) }) : undefined"
          :data-custom-id="c.custom_id"
          data-testid="component-button"
          @click="press(c)"
        >
          <span v-if="c.emoji?.name && c.label" aria-hidden="true">{{ c.emoji.name }}</span>
          <span class="min-w-0">{{ buttonText(c) }}</span>
          <ExternalLink v-if="c.style === LINK_STYLE" class="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
        </button>
      </template>
    </div>
    <p v-if="read.skipped" class="text-xs italic text-mnema-tertiary" data-testid="component-unsupported">{{ $t('components.unsupported') }}</p>
    <p v-if="error" role="status" class="text-xs text-mnema-tertiary" data-testid="component-error">{{ error }}</p>
  </div>
</template>
