<script setup lang="ts">
import type { PropType } from 'vue'
import { computed } from 'vue'
import { ChartNoAxesColumn, Check, EyeOff } from '@lucide/vue'
import type { Message } from '../types/domain'
import { usePoll } from '../composables/usePoll'
import { useChatStore } from '../stores/chat'
import { confirm } from '../lib/confirm'
import { leaders, percent } from '../lib/polls'
import { shownName } from '../lib/deletedUser'
import { t } from '../i18n'

// The options of a poll message under its question (the message text): a
// button per answer with a live bar, who voted when the poll is open about it,
// and the time left. Voting is server-checked; the card only offers.
const props = defineProps({
  msg: { type: Object as PropType<Message>, required: true }
})

const chat = useChatStore()
const { poll, ended, left, mine, busy, canVote, canEnd, vote, end } = usePoll(() => props.msg)
const winners = computed(() => (ended.value ? leaders(poll.value) : []))
const interactive = computed(() => canVote.value && !busy.value)

const MAX_NAMES = 3
function voterNames(ids: readonly string[] | undefined): string {
  if (!ids?.length) return ''
  const names = ids.slice(0, MAX_NAMES).map(id => {
    const m = chat.members.find(x => x.id === id)
    return m ? shownName(m) : t('poll.someone')
  })
  return ids.length > MAX_NAMES ? `${names.join(', ')} ${t('poll.more', { count: ids.length - MAX_NAMES })}` : names.join(', ')
}

async function confirmEnd() {
  const ok = await confirm({ title: t('poll.endTitle'), body: t('poll.endBody'), confirmLabel: t('poll.end'), danger: true })
  if (ok) await end()
}
</script>

<template>
  <section data-testid="poll" class="mt-1.5 max-w-md rounded-lg border border-mnema-border bg-mnema-raised p-3 select-text" :aria-label="$t('poll.title')">
    <p class="mb-2 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-mnema-tertiary">
      <ChartNoAxesColumn class="h-3.5 w-3.5 text-mnema-accent" aria-hidden="true" />
      <span class="font-medium uppercase tracking-wide">{{ $t('poll.title') }}</span>
      <span>{{ poll.multiple ? $t('poll.multiple') : $t('poll.single') }}</span>
      <span v-if="poll.anonymous" class="inline-flex items-center gap-1" data-testid="poll-anonymous"><EyeOff class="h-3 w-3" aria-hidden="true" />{{ $t('poll.anonymous') }}</span>
    </p>

    <ul class="space-y-1.5" role="group" :aria-label="$t('poll.options')">
      <li v-for="o in poll.options" :key="o.id">
        <button
          type="button"
          :data-option="o.id"
          :aria-pressed="mine.includes(o.id) ? 'true' : 'false'"
          :disabled="!interactive"
          :class="[
            'relative block w-full overflow-hidden rounded-md border px-2.5 py-1.5 text-left text-sm transition',
            mine.includes(o.id) ? 'border-mnema-accent' : 'border-mnema-hairline',
            interactive ? 'cursor-pointer hover:border-mnema-accent/70' : 'cursor-default'
          ]"
          @click="vote(o.id)"
        >
          <span
            aria-hidden="true"
            data-testid="poll-bar"
            :class="['absolute inset-y-0 left-0 transition-[width]', winners.includes(o.id) ? 'bg-mnema-accent/30' : 'bg-mnema-accent/15']"
            :style="{ width: `${percent(o.votes, poll.total_voters)}%` }"
          />
          <span class="relative flex items-center gap-2">
            <Check v-if="mine.includes(o.id)" class="h-3.5 w-3.5 flex-shrink-0 text-mnema-accent" aria-hidden="true" />
            <span class="min-w-0 flex-1 break-words text-mnema-text" :class="winners.includes(o.id) ? 'font-semibold' : ''">{{ o.label }}</span>
            <span class="flex-shrink-0 text-xs tabular-nums text-mnema-muted">{{ o.votes }} · {{ percent(o.votes, poll.total_voters) }}%</span>
          </span>
        </button>
        <p v-if="!poll.anonymous && o.voters?.length" class="mt-0.5 truncate px-1 text-xs text-mnema-tertiary" data-testid="poll-voters">{{ voterNames(o.voters) }}</p>
      </li>
    </ul>

    <div class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-mnema-tertiary">
      <span data-testid="poll-total">{{ $t('poll.voters', { count: poll.total_voters }) }}</span>
      <span aria-hidden="true">·</span>
      <span v-if="ended" data-testid="poll-ended">{{ $t('poll.ended') }}</span>
      <span v-else-if="left" data-testid="poll-left">{{ $t(`poll.left.${left.unit}`, { count: left.count }) }}</span>
      <button
        v-if="canEnd"
        type="button"
        data-testid="poll-end"
        class="ml-auto rounded px-1.5 py-0.5 font-medium text-mnema-muted transition hover:bg-mnema-surface hover:text-mnema-text"
        :disabled="busy"
        @click="confirmEnd"
      >{{ $t('poll.end') }}</button>
    </div>
  </section>
</template>
