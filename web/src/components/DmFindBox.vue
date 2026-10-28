<script setup lang="ts">
// "Find or start a conversation": type a name to see the conversations that
// match and the members you have none with yet. Enter takes the first,
// arrows move, Escape clears. Starting one opens it at once.
import { computed, ref } from 'vue'
import { Search } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useBlocksStore } from '../stores/blocks'
import { useChatStore } from '../stores/chat'
import { useDmActions } from '../composables/useDmActions'
import { dmPeer, dmTitle } from '../lib/dm'
import { matchDms } from '../lib/dmList'
import UserAvatar from './UserAvatar.vue'
import DmAvatar from './DmAvatar.vue'

const MAX_MEMBERS = 5

const emit = defineEmits<{ done: [] }>()

const authStore = useAuthStore()
const blocks = useBlocksStore()
const chatStore = useChatStore()
const { canMessage, messageMember } = useDmActions()

const query = ref('')
const active = ref(0)
const listId = 'dm-find-results'

const conversations = computed(() => matchDms(chatStore.dms, authStore.user?.id, query.value))
const members = computed(() => {
  const needle = query.value.trim().toLowerCase()
  if (!needle) return []
  const talking = new Set(chatStore.dms.map(dm => dmPeer(dm, authStore.user?.id)?.id))
  return chatStore.members
    .filter(m => !talking.has(m.id) && !blocks.isBlocked(m.id) && canMessage(m.id) && `${m.display_name} ${m.username}`.toLowerCase().includes(needle))
    .slice(0, MAX_MEMBERS)
})

type Choice = { kind: 'dm'; id: string } | { kind: 'member'; id: string }
const choices = computed<Choice[]>(() => [
  ...conversations.value.map(dm => ({ kind: 'dm' as const, id: dm.id })),
  ...members.value.map(m => ({ kind: 'member' as const, id: m.id }))
])
const optionId = (i: number) => `dm-find-option-${i}`

async function pick(choice: Choice | undefined) {
  if (!choice) return
  if (choice.kind === 'dm') await chatStore.openDm(choice.id)
  else if (!(await messageMember(choice.id))) return
  query.value = ''
  active.value = 0
  emit('done')
}

function onKey(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    if (query.value) {
      query.value = ''
      event.stopPropagation()
    }
  } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    const n = choices.value.length
    if (n) active.value = (active.value + (event.key === 'ArrowDown' ? 1 : n - 1)) % n
  } else if (event.key === 'Enter') {
    // Enter that commits an IME composition is not a choice.
    if (event.isComposing || event.keyCode === 229) return
    event.preventDefault()
    void pick(choices.value[active.value])
  }
}
</script>

<template>
  <div class="px-1 pb-1" data-testid="dm-find">
    <div class="relative">
      <Search class="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-mnema-tertiary" aria-hidden="true" />
      <input
        v-model="query"
        type="search"
        role="combobox"
        autocomplete="off"
        data-testid="dm-find-input"
        :aria-label="$t('dm.find')"
        :placeholder="$t('dm.find')"
        :aria-expanded="choices.length > 0 ? 'true' : 'false'"
        :aria-controls="listId"
        :aria-activedescendant="choices.length ? optionId(active) : undefined"
        class="h-7 w-full rounded-md border border-mnema-border bg-mnema-canvas pl-7 pr-2 text-xs text-mnema-text placeholder:text-mnema-tertiary focus:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent max-md:h-11 max-md:text-sm"
        @input="active = 0"
        @keydown="onKey"
      />
    </div>
    <ul v-if="query.trim()" :id="listId" role="listbox" class="mt-1" :aria-label="$t('dm.find')">
      <li
        v-for="(choice, i) in choices"
        :id="optionId(i)"
        :key="`${choice.kind}-${choice.id}`"
        role="option"
        :aria-selected="i === active ? 'true' : 'false'"
        :data-find="choice.kind"
        :class="['flex min-h-8 cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-nav max-md:min-h-11', i === active ? 'bg-mnema-hover text-mnema-text' : 'text-mnema-muted hover:bg-mnema-hover/70']"
        @mousedown.prevent
        @click="pick(choice)"
      >
        <template v-if="choice.kind === 'dm'">
          <DmAvatar v-if="chatStore.dmById(choice.id)" :dm="chatStore.dmById(choice.id)!" />
          <span class="truncate">{{ dmTitle(chatStore.dmById(choice.id) ?? { members: [] }, authStore.user?.id) }}</span>
        </template>
        <template v-else>
          <UserAvatar :user="chatStore.members.find(m => m.id === choice.id) ?? null" size="sm" />
          <span class="min-w-0 flex-1 truncate">{{ chatStore.members.find(m => m.id === choice.id)?.display_name }}</span>
          <span class="shrink-0 text-xs text-mnema-tertiary">{{ $t('dm.findStart') }}</span>
        </template>
      </li>
      <li v-if="!choices.length" class="px-2 py-1 text-xs italic text-mnema-tertiary" data-testid="dm-find-empty">{{ $t('dm.findNone') }}</li>
    </ul>
  </div>
</template>
