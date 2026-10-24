<script setup lang="ts">
// The members this member blocked, with a way to lift each block. Blocking is
// private: the blocked member is never told, and nothing they can read depends
// on it.
import { useBlocksStore } from '../stores/blocks'
import UserAvatar from './UserAvatar.vue'

const blocks = useBlocksStore()
</script>

<template>
  <section class="settings-card" data-testid="blocked-list" aria-labelledby="blocked-title">
    <div>
      <h3 id="blocked-title" class="settings-title">{{ $t('blocks.title') }}</h3>
      <p class="settings-help">{{ $t('blocks.intro') }}</p>
    </div>
    <ul v-if="blocks.blocked.length" class="space-y-2">
      <li v-for="member in blocks.blocked" :key="member.user_id" class="flex items-center gap-3 rounded-md border border-mnema-hairline px-4 py-2.5" :data-blocked="member.username">
        <UserAvatar :user="{ username: member.username, display_name: member.display_name, avatar_url: member.avatar_url }" size="xs" />
        <span class="min-w-0 flex-1 truncate text-sm text-mnema-text">{{ member.display_name || member.username }}</span>
        <button
          type="button"
          data-testid="blocked-unblock"
          class="btn btn-secondary max-md:min-h-11"
          @click="blocks.unblock(member.user_id)"
        >{{ $t('blocks.unblock') }}</button>
      </li>
    </ul>
    <p v-else class="text-sm italic text-mnema-tertiary" data-testid="blocked-empty">{{ $t('blocks.none') }}</p>
  </section>
</template>
