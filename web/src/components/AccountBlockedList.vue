<script setup lang="ts">
// The members this member blocked, with a way to lift each block. Blocking is
// private: the blocked member is never told, and nothing they can read depends
// on it.
import { useBlocksStore } from '../stores/blocks'
import UserAvatar from './UserAvatar.vue'

const blocks = useBlocksStore()
</script>

<template>
  <div class="space-y-2" data-testid="blocked-list">
    <p class="text-sm font-medium text-mnema-text">{{ $t('blocks.title') }}</p>
    <p class="text-sm text-mnema-muted">{{ $t('blocks.intro') }}</p>
    <ul v-if="blocks.blocked.length" class="space-y-1">
      <li v-for="member in blocks.blocked" :key="member.user_id" class="flex items-center gap-3 rounded-md border border-mnema-hairline px-3 py-1.5" :data-blocked="member.username">
        <UserAvatar :user="{ username: member.username, display_name: member.display_name, avatar_url: member.avatar_url }" size="xs" />
        <span class="min-w-0 flex-1 truncate text-sm text-mnema-text">{{ member.display_name || member.username }}</span>
        <button
          type="button"
          data-testid="blocked-unblock"
          class="btn btn-secondary btn-sm max-md:min-h-11"
          @click="blocks.unblock(member.user_id)"
        >{{ $t('blocks.unblock') }}</button>
      </li>
    </ul>
    <p v-else class="text-sm italic text-mnema-tertiary" data-testid="blocked-empty">{{ $t('blocks.none') }}</p>
  </div>
</template>
