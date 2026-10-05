<script setup>
// One member in the member list: avatar with live status, display name and
// a second line (Talk, status text or presence). Click opens the profile.
import { computed } from 'vue'
import { Crown, Volume2 } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { t } from '../i18n'
import UserAvatar from './UserAvatar.vue'

const props = defineProps({
  member: { type: Object, required: true },
  // Talk the member is in, if any.
  voiceChannel: { type: String, default: '' }
})
const emit = defineEmits(['menu'])

const chatStore = useChatStore()
const voiceStore = useVoiceStore()

const status = computed(() => chatStore.presenceOf(props.member.id))
const offline = computed(() => status.value === 'offline')
const subline = computed(() => {
  if (offline.value) return ''
  return props.member.status_text || (props.member.role === 'admin' ? t(`presence.${status.value}`) : '')
})
</script>

<template>
  <div
    role="button"
    tabindex="0"
    aria-haspopup="menu"
    :data-member="member.username"
    :data-status="status"
    class="group flex h-[42px] min-w-0 cursor-pointer items-center gap-3 rounded-md px-2 transition-colors hover:bg-mnema-hover focus:outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent"
    @click="chatStore.openUserProfile(member)"
    @keydown.enter.self.prevent="chatStore.openUserProfile(member)"
    @contextmenu="emit('menu', $event)"
    @keydown.f10.shift.self.prevent="emit('menu', $event)"
    @keydown.context-menu.self.prevent="emit('menu', $event)"
  >
    <UserAvatar
      :user="member"
      size="sm"
      show-status
      :status="status"
      ring-class="bg-mnema-raised"
      :is-speaking="!!voiceStore.speakingUsers[member.id]"
    />

    <div class="flex min-w-0 flex-1 flex-col">
      <div class="flex min-w-0 items-center gap-1">
        <span
          :class="[
            'truncate text-nav transition-colors group-hover:text-mnema-accent',
            offline ? 'text-mnema-muted' : 'font-medium text-mnema-text',
            member.role === 'admin' && !offline ? 'font-semibold' : ''
          ]"
        >{{ member.display_name || member.username }}</span>
        <Crown v-if="member.role === 'admin'" class="h-3.5 w-3.5 flex-shrink-0 text-mnema-amber" />
      </div>

      <div v-if="voiceChannel" class="flex min-w-0 items-center gap-1 text-xs font-medium text-mnema-mint">
        <Volume2 class="h-3.5 w-3.5 flex-shrink-0" />
        <span class="truncate">{{ voiceChannel }}</span>
      </div>
      <span v-else-if="subline" class="truncate text-xs text-mnema-tertiary">{{ subline }}</span>
    </div>
  </div>
</template>
