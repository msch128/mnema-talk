<script setup lang="ts">
import type { PropType } from 'vue'
import type { AvatarUser, AvatarSize, LivePresence } from './presentationTypes'
import { computed } from 'vue'
import PresenceDot from './PresenceDot.vue'

const props = defineProps({
  user: {
    type: Object as PropType<AvatarUser | null>,
    default: () => ({})
  },
  size: {
    type: String as PropType<AvatarSize>,
    default: 'md' // 'xxs' | 'xs' | 'sm' | 'md' | 'lg' | 'xl'
  },
  showStatus: {
    type: Boolean,
    default: false
  },
  isOnline: {
    type: Boolean,
    default: false
  },
  // Live status (online, away, dnd, focus, offline); wins over isOnline.
  status: {
    type: String as PropType<LivePresence | ''>,
    default: ''
  },
  // Background behind the avatar, so the status dot's ring blends in.
  ringClass: {
    type: String,
    default: 'bg-mnema-canvas'
  },
  isSpeaking: {
    type: Boolean,
    default: false
  }
})

const sizeClasses = computed(() => {
  switch (props.size) {
    case 'xxs': return 'w-4 h-4 text-xs leading-none'
    case 'xs': return 'w-6 h-6 text-xs'
    case 'sm': return 'w-8 h-8 text-xs'
    case 'md': return 'w-10 h-10 text-sm'
    case 'lg': return 'w-12 h-12 text-base'
    case 'xl': return 'w-16 h-16 text-lg'
    default: return 'w-10 h-10 text-sm'
  }
})

const initial = computed(() => {
  const name = props.user?.display_name || props.user?.username || '?'
  return name.charAt(0).toUpperCase()
})

const dotStatus = computed(() => props.status || (props.isOnline ? 'online' : 'offline'))
const dotSize = computed(() => (props.size === 'xl' ? 12 : props.size === 'lg' ? 10 : 8))

const avatarUrl = computed(() => {
  return props.user?.avatar_url || ''
})
</script>

<template>
  <div class="relative inline-block flex-shrink-0">
    <div 
      :class="[
        'rounded-full flex items-center justify-center font-semibold overflow-hidden border transition-all select-none',
        sizeClasses,
        isSpeaking ? 'ring-2 ring-mnema-accent ring-offset-1 ring-offset-mnema-canvas' : '',
        avatarUrl ? 'border-mnema-border bg-mnema-surface' : 'bg-mnema-band border-mnema-mint/30 text-mnema-mint'
      ]"
    >
      <img 
        v-if="avatarUrl" 
        :src="avatarUrl" 
        :alt="user?.display_name || $t('user.avatar')"
        class="w-full h-full object-cover"
        loading="lazy"
      />
      <span v-else>{{ initial }}</span>
    </div>

    <PresenceDot
      v-if="showStatus"
      :status="dotStatus"
      :size="dotSize"
      :ring-class="ringClass"
      class="absolute -bottom-0.5 -right-0.5"
    />
  </div>
</template>
