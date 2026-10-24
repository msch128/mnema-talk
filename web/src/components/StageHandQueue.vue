<script setup lang="ts">
import type { PropType } from 'vue'
import type { TalkParticipant } from './presentationTypes'
// The raised hands of a Stage, the one waiting longest first. Only shown to
// members who hold Manage stage: accept gives talk power in one click,
// dismiss lowers the hand.
import { Check, Hand, X } from '@lucide/vue'
import UserAvatar from './UserAvatar.vue'
import { focusRing } from './talkButtons'

defineProps({
  hands: { type: Array as PropType<TalkParticipant[]>, required: true }
})
const emit = defineEmits<{ accept: [userId: string]; dismiss: [userId: string] }>()

const name = (user: TalkParticipant) => user.display_name || user.username
</script>

<template>
  <section data-testid="stage-hands" :aria-label="$t('stage.handsTitle')" class="w-full rounded-lg border border-mnema-border bg-mnema-elevated/60 p-3">
    <h3 class="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-mnema-tertiary">
      <Hand class="h-3.5 w-3.5" aria-hidden="true" />
      <span>{{ $t('stage.handsTitle') }}</span>
      <span class="rounded-full bg-mnema-accent-subtle px-1.5 font-mono text-mnema-accent" data-testid="stage-hands-count">{{ hands.length }}</span>
    </h3>
    <p v-if="!hands.length" class="text-sm text-mnema-tertiary">{{ $t('stage.noHands') }}</p>
    <ul v-else class="stack-1.5">
      <li v-for="user in hands" :key="user.id" data-testid="stage-hand" class="flex items-center gap-2">
        <UserAvatar :user="user" size="xs" />
        <span class="min-w-0 flex-1 truncate text-sm font-medium text-mnema-text">{{ name(user) }}</span>
        <button
          type="button"
          data-testid="stage-accept"
          :aria-label="$t('stage.accept', { name: name(user) })"
          v-tooltip="$t('stage.accept', { name: name(user) })"
          :class="['inline-flex h-8 items-center gap-1 rounded-full bg-mnema-accent px-3 text-xs font-semibold text-mnema-accent-ink transition-colors hover:bg-mnema-accent-hover motion-reduce:transition-none', focusRing]"
          @click="emit('accept', user.id)"
        >
          <Check class="h-4 w-4" aria-hidden="true" />
          <span>{{ $t('stage.acceptShort') }}</span>
        </button>
        <button
          type="button"
          data-testid="stage-dismiss"
          :aria-label="$t('stage.dismiss', { name: name(user) })"
          v-tooltip="$t('stage.dismiss', { name: name(user) })"
          :class="['inline-flex h-8 w-8 items-center justify-center rounded-full bg-mnema-surface text-mnema-tertiary transition-colors hover:bg-mnema-hover hover:text-mnema-text motion-reduce:transition-none', focusRing]"
          @click="emit('dismiss', user.id)"
        >
          <X class="h-4 w-4" aria-hidden="true" />
        </button>
      </li>
    </ul>
  </section>
</template>
