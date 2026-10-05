<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false
  },
  x: {
    type: Number,
    default: 0
  },
  y: {
    type: Number,
    default: 0
  },
  items: {
    type: Array,
    default: () => []
  },
  minWidth: {
    type: Number,
    default: 180
  }
})

const emit = defineEmits(['update:modelValue', 'close', 'select'])

const menuEl = ref(null)
const posX = ref(props.x)
const posY = ref(props.y)
const activeIndex = ref(-1)

const navigableItems = computed(() => {
  return props.items.filter(item => item.type !== 'separator' && item.type !== 'slider')
})

function adjustPosition() {
  if (!menuEl.value) return
  const rect = menuEl.value.getBoundingClientRect()
  const pad = 8
  const winW = window.innerWidth
  const winH = window.innerHeight

  let newX = props.x
  let newY = props.y

  if (newX + rect.width > winW - pad) {
    newX = Math.max(pad, winW - rect.width - pad)
  }
  if (newY + rect.height > winH - pad) {
    newY = Math.max(pad, winH - rect.height - pad)
  }

  posX.value = newX
  posY.value = newY
}

watch(() => props.modelValue, (open) => {
  if (open) {
    posX.value = props.x
    posY.value = props.y
    activeIndex.value = -1
    nextTick(() => {
      adjustPosition()
      menuEl.value?.focus()
    })
  }
})

watch([() => props.x, () => props.y], () => {
  if (props.modelValue) {
    posX.value = props.x
    posY.value = props.y
    nextTick(adjustPosition)
  }
})

function close() {
  emit('update:modelValue', false)
  emit('close')
}

function handleSelect(item) {
  if (item.disabled) return
  if (item.action) item.action()
  emit('select', item)
  close()
}

function handleKeydown(e) {
  if (e.key === 'Escape') {
    e.preventDefault()
    close()
    return
  }

  const items = navigableItems.value
  if (!items.length) return

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    activeIndex.value = (activeIndex.value + 1) % items.length
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    activeIndex.value = (activeIndex.value - 1 + items.length) % items.length
  } else if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    if (activeIndex.value >= 0 && activeIndex.value < items.length) {
      handleSelect(items[activeIndex.value])
    }
  }
}

function handleClickOutside(e) {
  if (props.modelValue && menuEl.value && !menuEl.value.contains(e.target)) {
    close()
  }
}

function handleScroll() {
  if (props.modelValue) close()
}

onMounted(() => {
  document.addEventListener('mousedown', handleClickOutside)
  document.addEventListener('contextmenu', handleClickOutside)
  window.addEventListener('scroll', handleScroll, true)
})

onUnmounted(() => {
  document.removeEventListener('mousedown', handleClickOutside)
  document.removeEventListener('contextmenu', handleClickOutside)
  window.removeEventListener('scroll', handleScroll, true)
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue"
      ref="menuEl"
      role="menu"
      tabindex="-1"
      :style="{
        left: `${posX}px`,
        top: `${posY}px`,
        minWidth: `${minWidth}px`
      }"
      class="fixed z-50 p-1.5 rounded-lg bg-mnema-elevated border border-mnema-border shadow-2xl focus:outline-none select-none text-sm text-mnema-muted font-sans animate-in fade-in zoom-in-95 duration-100"
      @keydown="handleKeydown"
    >
      <slot name="header"></slot>

      <template v-for="(item, idx) in items" :key="idx">
        <div
          v-if="item.type === 'separator'"
          role="separator"
          class="my-1 h-px bg-mnema-hairline"
        ></div>

        <div
          v-else-if="item.type === 'slider'"
          class="px-2.5 py-1.5 flex flex-col gap-1 select-none"
          @mousedown.stop
          @click.stop
        >
          <div class="flex items-center justify-between text-xs text-mnema-muted font-normal">
            <span class="truncate">{{ item.label }}</span>
            <span class="font-mono text-mnema-tertiary ml-2 flex-shrink-0">{{ item.value }}%</span>
          </div>
          <input
            type="range"
            :min="item.min ?? 0"
            :max="item.max ?? 200"
            :step="item.step ?? 1"
            :value="item.value"
            @input="item.onInput?.(Number($event.target.value))"
            class="w-full h-1.5 bg-mnema-surface rounded-lg appearance-none cursor-pointer accent-mnema-accent"
          />
        </div>

        <button
          v-else
          type="button"
          role="menuitem"
          tabindex="-1"
          :disabled="item.disabled"
          :class="[
            'w-full h-8 px-2.5 flex items-center justify-between gap-3 rounded text-left transition-colors font-normal',
            item.disabled ? 'opacity-40 cursor-not-allowed' : 'cursor-pointer',
            item.danger
              ? 'text-red-400 hover:bg-red-500/10 hover:text-red-300'
              : 'hover:bg-mnema-hover hover:text-mnema-text',
            navigableItems[activeIndex] === item
              ? (item.danger ? 'bg-red-500/10 text-red-300' : 'bg-mnema-hover text-mnema-text')
              : ''
          ]"
          @click="handleSelect(item)"
        >
          <div class="flex items-center gap-2 truncate">
            <component :is="item.icon" v-if="item.icon" class="w-4 h-4 flex-shrink-0 opacity-80" />
            <span class="truncate">{{ item.label }}</span>
          </div>

          <span v-if="item.shortcut" class="text-xs text-mnema-tertiary ml-auto font-mono flex-shrink-0">
            {{ item.shortcut }}
          </span>
        </button>
      </template>

      <slot name="footer"></slot>
    </div>
  </Teleport>
</template>
