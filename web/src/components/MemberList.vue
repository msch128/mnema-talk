<script setup>
import { computed } from 'vue'
import { Crown, Volume2 } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import UserAvatar from './UserAvatar.vue'
import ContextMenu from './ContextMenu.vue'
import { useMenuState, buildMemberItems } from '../composables/useNavMenus'
import { t } from '../i18n'

const chatStore = useChatStore()
const voiceStore = useVoiceStore()

// Group members into admins, online members and offline members
const admins = computed(() => {
  return chatStore.members.filter(m => m.role === 'admin')
})

const onlineNonAdmins = computed(() => {
  return chatStore.onlineMembers.filter(m => m.role !== 'admin')
})

const offlineNonAdmins = computed(() => {
  return chatStore.offlineMembers.filter(m => m.role !== 'admin')
})

const menu = useMenuState()

function openMemberMenu(e, member) {
  menu.show(e, refresh => buildMemberItems(member, { refresh }))
}

function getUserVoiceChannel(userId) {
  for (const [chId, users] of Object.entries(voiceStore.channelUsers)) {
    if (users && users[userId]) {
      for (const cat of chatStore.categories) {
        const ch = cat.channels?.find(c => c.id === chId)
        if (ch) return ch.name
      }
      const uncat = chatStore.uncategorized?.find(c => c.id === chId)
      if (uncat) return uncat.name
      return t('voice.channelFallback')
    }
  }
  return null
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

        <div
          v-for="member in admins"
          :key="member.id"
          role="button"
          tabindex="0"
          aria-haspopup="menu"
          @click="chatStore.openUserProfile(member)"
          @keydown.enter.self.prevent="chatStore.openUserProfile(member)"
          @contextmenu="openMemberMenu($event, member)"
          @keydown.f10.shift.self.prevent="openMemberMenu($event, member)"
          @keydown.context-menu.self.prevent="openMemberMenu($event, member)"
          class="h-[42px] flex items-center gap-3 px-2 rounded-md hover:bg-mnema-hover transition-colors group cursor-pointer min-w-0 focus:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent"
        >
          <UserAvatar
            :user="member"
            size="sm"
            :show-status="true"
            :is-online="chatStore.onlineUserIds.has(member.id)"
            :is-speaking="!!voiceStore.speakingUsers[member.id]"
          />

          <div class="flex flex-col min-w-0 flex-1">
            <div class="flex items-center gap-1 min-w-0">
              <span class="text-nav font-semibold text-mnema-text group-hover:text-mnema-accent transition-colors truncate">
                {{ member.display_name }}
              </span>
              <Crown class="w-3.5 h-3.5 text-mnema-amber flex-shrink-0" />
            </div>

            <div v-if="getUserVoiceChannel(member.id)" class="flex items-center gap-1 text-xs text-mnema-mint font-medium min-w-0">
              <Volume2 class="w-3.5 h-3.5 flex-shrink-0" />
              <span class="truncate">{{ getUserVoiceChannel(member.id) }}</span>
            </div>
            <span v-else class="text-xs text-mnema-tertiary truncate">
              {{ chatStore.onlineUserIds.has(member.id) ? $t('presence.online') : $t('presence.offline') }}
            </span>
          </div>
        </div>
      </section>

      <!-- 2. Online Members Group -->
      <section v-if="onlineNonAdmins.length">
        <h3 class="pt-6 pb-1 px-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary truncate">
          {{ $t('members.online', { count: onlineNonAdmins.length }) }}
        </h3>

        <div
          v-for="member in onlineNonAdmins"
          :key="member.id"
          role="button"
          tabindex="0"
          aria-haspopup="menu"
          @click="chatStore.openUserProfile(member)"
          @keydown.enter.self.prevent="chatStore.openUserProfile(member)"
          @contextmenu="openMemberMenu($event, member)"
          @keydown.f10.shift.self.prevent="openMemberMenu($event, member)"
          @keydown.context-menu.self.prevent="openMemberMenu($event, member)"
          class="h-[42px] flex items-center gap-3 px-2 rounded-md hover:bg-mnema-hover transition-colors group cursor-pointer min-w-0 focus:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent"
        >
          <UserAvatar
            :user="member"
            size="sm"
            :show-status="true"
            :is-online="true"
            :is-speaking="!!voiceStore.speakingUsers[member.id]"
          />

          <div class="flex flex-col min-w-0 flex-1">
            <span class="text-nav font-medium text-mnema-text group-hover:text-mnema-accent transition-colors truncate">
              {{ member.display_name }}
            </span>

            <div v-if="getUserVoiceChannel(member.id)" class="flex items-center gap-1 text-xs text-mnema-mint font-medium min-w-0">
              <Volume2 class="w-3.5 h-3.5 flex-shrink-0" />
              <span class="truncate">{{ getUserVoiceChannel(member.id) }}</span>
            </div>
          </div>
        </div>
      </section>

      <!-- 3. Offline Members Group -->
      <section v-if="offlineNonAdmins.length">
        <h3 class="pt-6 pb-1 px-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary truncate">
          {{ $t('members.offline', { count: offlineNonAdmins.length }) }}
        </h3>

        <div class="opacity-60 hover:opacity-100 transition-opacity">
          <div
            v-for="member in offlineNonAdmins"
            :key="member.id"
            role="button"
          tabindex="0"
          aria-haspopup="menu"
          @click="chatStore.openUserProfile(member)"
          @keydown.enter.self.prevent="chatStore.openUserProfile(member)"
          @contextmenu="openMemberMenu($event, member)"
          @keydown.f10.shift.self.prevent="openMemberMenu($event, member)"
          @keydown.context-menu.self.prevent="openMemberMenu($event, member)"
            class="h-[42px] flex items-center gap-3 px-2 rounded-md hover:bg-mnema-hover transition-colors group cursor-pointer min-w-0 focus:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent"
          >
            <UserAvatar
              :user="member"
              size="sm"
              :show-status="true"
              :is-online="false"
            />

            <span class="text-nav text-mnema-muted group-hover:text-mnema-accent transition-colors truncate min-w-0 flex-1">
              {{ member.display_name }}
            </span>
          </div>
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
