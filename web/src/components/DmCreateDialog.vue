<script setup lang="ts">
import type { PropType } from 'vue'
import type { ChatDirectMessage } from '../types/rest'
// The member picker: choose one member for a conversation of two, several for
// a group (up to ten people in all), or, given a group, the member to add.
// Which members are offered is the community's list; whether a conversation
// can be started with one is up to the server, which answers every refusal
// in the same words.
import { computed, ref } from 'vue'
import { Check, Loader2 } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useBlocksStore } from '../stores/blocks'
import { useChatStore } from '../stores/chat'
import { DM_MAX_MEMBERS, dmErrorText, dmTitle } from '../lib/dm'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import UserAvatar from './UserAvatar.vue'

const props = defineProps({
  // Given: add one member to this group instead of starting a conversation.
  group: { type: Object as PropType<ChatDirectMessage | null>, default: null }
})
const emit = defineEmits<{ close: [] }>()

const authStore = useAuthStore()
const blocks = useBlocksStore()
const chatStore = useChatStore()

const query = ref('')
const picked = ref<string[]>([])
const busy = ref(false)
const error = ref('')

const adding = computed(() => props.group !== null)
// Seats left besides the caller (starting) or in the group (adding).
const seats = computed(() => (props.group ? DM_MAX_MEMBERS - props.group.members.length : DM_MAX_MEMBERS - 1))
const taken = computed(() => new Set(props.group?.members.map(m => m.id) ?? []))

const candidates = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return chatStore.members
    .filter(m => m.id !== authStore.user?.id && !m.is_bot && !taken.value.has(m.id) && !blocks.isBlocked(m.id))
    .filter(m => !needle || `${m.display_name} ${m.username}`.toLowerCase().includes(needle))
    .sort((a, b) => (a.display_name || a.username).localeCompare(b.display_name || b.username))
})

const title = computed(() => (props.group ? t('dm.addTitle', { name: dmTitle(props.group, authStore.user?.id) }) : t('dm.newTitle')))
const submitLabel = computed(() => {
  if (adding.value) return t('dm.add')
  return picked.value.length > 1 ? t('dm.startGroup') : t('dm.start')
})

function toggle(id: string) {
  error.value = ''
  if (picked.value.includes(id)) {
    picked.value = picked.value.filter(p => p !== id)
  } else if (adding.value) {
    picked.value = [id]
  } else if (picked.value.length < seats.value) {
    picked.value = [...picked.value, id]
  }
}

async function submit() {
  if (!picked.value.length || busy.value) return
  busy.value = true
  error.value = ''
  try {
    if (props.group) {
      await chatStore.addDmMember(props.group.id, picked.value[0] ?? '')
    } else {
      const dm = await chatStore.startDm(picked.value)
      // The picker goes away at once; the conversation loads behind it.
      emit('close')
      void chatStore.openDm(dm.id)
      return
    }
    emit('close')
  } catch (err) {
    error.value = dmErrorText(err, adding.value ? 'add' : 'start')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseDialog :title="title" panel-class="max-w-md" initial-focus="[data-dm-search]" @close="emit('close')">
    <form class="flex min-h-0 flex-col gap-3 p-5" data-testid="dm-create" @submit.prevent="submit">
      <p class="text-sm text-mnema-muted">{{ $t(adding ? 'dm.addHint' : 'dm.newHint') }}</p>
      <input
        v-model="query"
        data-dm-search
        type="search"
        autocomplete="off"
        spellcheck="false"
        :aria-label="$t('dm.search')"
        :placeholder="$t('dm.search')"
        class="w-full rounded-lg border border-mnema-border bg-mnema-canvas px-3 py-2 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
      />

      <ul class="max-h-64 min-h-[6rem] space-y-0.5 overflow-y-auto" :aria-label="$t('dm.members')" data-testid="dm-candidates">
        <li v-for="member in candidates" :key="member.id">
          <button
            type="button"
            :aria-pressed="picked.includes(member.id) ? 'true' : 'false'"
            :data-dm-candidate="member.username"
            :class="[
              'flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-left transition max-md:min-h-11 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent',
              picked.includes(member.id) ? 'bg-mnema-accent/15 text-mnema-text' : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
            ]"
            @click="toggle(member.id)"
          >
            <UserAvatar :user="member" size="sm" />
            <span class="min-w-0 flex-1">
              <span class="block truncate text-sm font-medium">{{ member.display_name || member.username }}</span>
              <span class="block truncate font-mono text-xs text-mnema-tertiary">@{{ member.username }}</span>
            </span>
            <Check v-if="picked.includes(member.id)" class="h-4 w-4 shrink-0 text-mnema-accent" aria-hidden="true" />
          </button>
        </li>
        <li v-if="!candidates.length" class="px-2 py-3 text-sm italic text-mnema-tertiary">{{ $t('dm.noCandidates') }}</li>
      </ul>

      <p v-if="seats <= 0" class="text-sm text-mnema-muted" role="status">{{ $t('dm.groupFull') }}</p>
      <p v-else-if="!adding" class="text-xs text-mnema-tertiary" aria-live="polite" data-testid="dm-count">{{ $t('dm.picked', { n: picked.length, max: seats }) }}</p>
      <div v-if="error" role="alert" data-testid="dm-error" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-2 text-sm text-mnema-danger">{{ error }}</div>

      <div class="flex justify-end gap-2 border-t border-mnema-hairline pt-3">
        <button type="button" class="btn btn-secondary h-8 max-md:h-11" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button
          type="submit"
          :disabled="!picked.length || busy || seats <= 0"
          data-testid="dm-submit"
          class="btn btn-primary flex h-8 items-center gap-1.5 max-md:h-11"
        >
          <Loader2 v-if="busy" class="h-4 w-4 animate-spin" />
          {{ submitLabel }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>
