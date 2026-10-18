<script setup lang="ts">
import type { Member } from '../types/domain'
import { Crown } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { roleColorCss } from '../lib/permissions'
import MemberRow from './MemberRow.vue'
import ContextMenu from './ContextMenu.vue'
import { useMenuState, buildMemberItems } from '../composables/useNavMenus'
import { useMemberGroups } from '../composables/useMemberGroups'

const chatStore = useChatStore()
const { admins, roleGroups, onlineNonAdmins, offlineNonAdmins, voiceChannelName } = useMemberGroups()
const menu = useMenuState()

function openMemberMenu(e: MouseEvent | KeyboardEvent, member: Member) {
  menu.show(e, refresh => buildMemberItems(member, { refresh }))
}
</script>

<template>
  <!-- Member column; width and visibility are controlled by App.vue -->
  <aside class="w-full bg-mnema-raised border-l border-mnema-hairline flex flex-col h-full select-none">
    <!-- 48px header, aligned with the channel header and sidebar header -->
    <div class="h-12 px-4 border-b border-mnema-hairline flex items-center justify-between gap-2 flex-shrink-0">
      <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary truncate">
        {{ $t('members.title') }}
      </span>
      <span class="text-xs text-mnema-tertiary tabular-nums flex-shrink-0">
        {{ chatStore.members.length }}
      </span>
    </div>

    <!-- Scrollable Member Categories -->
    <div class="flex-1 overflow-y-auto overflow-x-hidden px-2 pb-4">
      <!-- 1. Administrators -->
      <section v-if="admins.length">
        <h3 class="pt-6 pb-1 px-2 text-xs font-semibold uppercase tracking-wide text-mnema-amber flex items-center gap-1.5 min-w-0">
          <Crown class="w-3.5 h-3.5 flex-shrink-0" />
          <span class="truncate">{{ $t('members.admins', { count: admins.length }) }}</span>
        </h3>
        <MemberRow
          v-for="member in admins"
          :key="member.id"
          :member="member"
          :voice-channel="voiceChannelName(member.id) || ''"
          @menu="openMemberMenu($event, member)"
        />
      </section>

      <!-- 2. Groups of hoisted roles -->
      <section v-for="group in roleGroups" :key="group.role.id" :data-role-group="group.role.name">
        <h3 class="pt-6 pb-1 px-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary flex items-center gap-1.5 min-w-0">
          <span class="h-2 w-2 flex-shrink-0 rounded-full border border-mnema-border-strong" :style="{ background: roleColorCss(group.role.color) || 'transparent' }"></span>
          <span class="truncate">{{ $t('members.roleGroup', { name: group.role.name, count: group.members.length }) }}</span>
        </h3>
        <MemberRow
          v-for="member in group.members"
          :key="member.id"
          :member="member"
          :voice-channel="voiceChannelName(member.id) || ''"
          @menu="openMemberMenu($event, member)"
        />
      </section>

      <!-- 3. Online Members Group -->
      <section v-if="onlineNonAdmins.length">
        <h3 class="pt-6 pb-1 px-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary truncate">
          {{ $t('members.online', { count: onlineNonAdmins.length }) }}
        </h3>
        <MemberRow
          v-for="member in onlineNonAdmins"
          :key="member.id"
          :member="member"
          :voice-channel="voiceChannelName(member.id) || ''"
          @menu="openMemberMenu($event, member)"
        />
      </section>

      <!-- 4. Offline Members Group -->
      <section v-if="offlineNonAdmins.length">
        <h3 class="pt-6 pb-1 px-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary truncate">
          {{ $t('members.offline', { count: offlineNonAdmins.length }) }}
        </h3>
        <div class="opacity-60 hover:opacity-100 transition-opacity">
          <MemberRow
            v-for="member in offlineNonAdmins"
            :key="member.id"
            :member="member"
            @menu="openMemberMenu($event, member)"
          />
        </div>
      </section>
    </div>

    <ContextMenu
      v-model="menu.state.open"
      :x="menu.state.x"
      :y="menu.state.y"
      :anchor="menu.state.anchor"
      :items="menu.items.value"
    />
  </aside>
</template>
