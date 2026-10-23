<script setup lang="ts">
import type { PropType } from 'vue'
import type { ChatDirectMessage } from '../types/rest'
// A conversation's members and what can be done with it: add someone to a
// group, leave a group, or (for a conversation of two, which cannot be left)
// block the other member. A member added to a group reads its whole history.
import { computed, ref } from 'vue'
import { LogOut, UserPlus } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useBlocksStore } from '../stores/blocks'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { confirm } from '../lib/confirm'
import { DM_MAX_MEMBERS, dmPeers, dmTitle } from '../lib/dm'
import { caughtErrorMessage } from '../lib/api'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import UserAvatar from './UserAvatar.vue'
import DmCreateDialog from './DmCreateDialog.vue'

const props = defineProps({
  dm: { type: Object as PropType<ChatDirectMessage>, required: true }
})
const emit = defineEmits<{ close: [] }>()

const authStore = useAuthStore()
const blocks = useBlocksStore()
const chatStore = useChatStore()
const toasts = useToastStore()

const adding = ref(false)
const title = computed(() => dmTitle(props.dm, authStore.user?.id))
const peer = computed(() => (props.dm.group ? null : dmPeers(props.dm, authStore.user?.id)[0] ?? null))
const canAdd = computed(() => props.dm.group && props.dm.can_send && props.dm.members.length < DM_MAX_MEMBERS)

async function leave() {
  const ok = await confirm({
    title: t('dm.leaveTitle'),
    body: t('dm.leaveBody'),
    confirmLabel: t('dm.leave'),
    cancelLabel: t('common.cancel'),
    danger: true
  })
  if (!ok) return
  try {
    await chatStore.leaveDm(props.dm.id)
    emit('close')
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('dm.leaveFailed')))
  }
}

function openProfile(member: ChatDirectMessage['members'][number]) {
  emit('close')
  chatStore.openUserProfile({ ...member, role: chatStore.members.find(m => m.id === member.id)?.role ?? 'user' })
}
</script>

<template>
  <BaseDialog :title="title" panel-class="max-w-sm" initial-focus="[data-dm-info-first]" @close="emit('close')">
    <div class="flex min-h-0 flex-col gap-4 p-5" data-testid="dm-info">
      <section :aria-label="$t('dm.members')">
        <h3 class="mb-1 text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('dm.membersCount', { n: dm.members.length }) }}</h3>
        <ul class="space-y-0.5">
          <li v-for="(member, i) in dm.members" :key="member.id">
            <button
              type="button"
              :data-dm-info-first="i === 0 ? '' : undefined"
              class="flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-left transition hover:bg-mnema-hover max-md:min-h-11 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent"
              @click="openProfile(member)"
            >
              <UserAvatar :user="member" size="sm" />
              <span class="min-w-0 flex-1 truncate text-sm font-medium text-mnema-text">{{ member.display_name || member.username }}</span>
              <span v-if="member.id === authStore.user?.id" class="text-xs text-mnema-tertiary">{{ $t('dm.you') }}</span>
            </button>
          </li>
        </ul>
      </section>

      <button
        v-if="canAdd"
        type="button"
        data-testid="dm-add-member"
        class="btn btn-secondary flex h-9 items-center justify-center gap-2 max-md:h-11"
        @click="adding = true"
      >
        <UserPlus class="h-4 w-4" />{{ $t('dm.addMember') }}
      </button>
      <p v-if="dm.group" class="text-xs text-mnema-tertiary">{{ $t('dm.historyHint') }}</p>

      <template v-if="dm.group">
        <button
          type="button"
          data-testid="dm-leave"
          class="btn btn-danger flex h-9 items-center justify-center gap-2 max-md:h-11"
          @click="leave"
        >
          <LogOut class="h-4 w-4" />{{ $t('dm.leave') }}
        </button>
      </template>
      <template v-else-if="peer">
        <p class="text-xs text-mnema-tertiary" data-testid="dm-pair-hint">{{ $t('dm.pairHint') }}</p>
        <button
          v-if="blocks.enabled && !blocks.isBlocked(peer.id)"
          type="button"
          data-testid="dm-block"
          class="btn btn-secondary flex h-9 items-center justify-center hover:border-mnema-danger hover:text-mnema-danger max-md:h-11"
          @click="blocks.block(peer.id)"
        >{{ $t('blocks.block') }}</button>
        <button
          v-else-if="blocks.enabled"
          type="button"
          data-testid="dm-unblock"
          class="btn btn-secondary flex h-9 items-center justify-center max-md:h-11"
          @click="blocks.unblock(peer.id)"
        >{{ $t('blocks.unblock') }}</button>
      </template>
    </div>
    <DmCreateDialog v-if="adding" :group="dm" @close="adding = false" />
  </BaseDialog>
</template>
