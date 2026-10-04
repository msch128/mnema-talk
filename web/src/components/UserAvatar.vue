<script setup>
import { computed } from 'vue'

const props = defineProps({
  user: {
    type: Object,
    default: () => ({})
  },
  size: {
    type: String,
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
        :alt="user?.display_name || 'Avatar'"
        class="w-full h-full object-cover"
        loading="lazy"
      />
      <span v-else>{{ initial }}</span>
    </div>

    <!-- Live Status Dot -->
    <span 
      v-if="showStatus"
      :class="[
        'absolute bottom-0 right-0 rounded-full border-2 border-mnema-canvas',
        size === 'xl' ? 'w-4 h-4' : 'w-2.5 h-2.5',
        isOnline ? 'bg-mnema-accent' : 'bg-mnema-muted/60'
      ]"
    ></span>
  </div>
</template>
