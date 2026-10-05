<script setup>
import { ref, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { Check } from '@lucide/vue'

// Context menu. Items: { label, icon, shortcut, action, danger, disabled },
// { type: 'separator' }, { type: 'label', label }, { type: 'slider', ... },
// { type: 'radio', label, checked, action }.
// Position either with x/y (viewport px) or with `anchor` (an element or a
// { left, top, right, bottom } rect), which is what keyboard openers use.
const props = defineProps({
  modelValue: { type: Boolean, default: false },
  x: { type: Number, default: 0 },
  y: { type: Number, default: 0 },
  anchor: { type: [Object, null], default: null },
  items: { type: Array, default: () => [] },
  minWidth: { type: Number, default: 180 },
  ariaLabel: { type: String, default: '' }
})

const emit = defineEmits(['update:modelValue', 'close', 'select'])

const menuEl = ref(null)
const posX = ref(props.x)
const posY = ref(props.y)
let opener = null

function basePoint() {
  const a = props.anchor
  if (a) {
    const r = typeof a.getBoundingClientRect === 'function' ? a.getBoundingClientRect() : a
    const left = r.left ?? r.x ?? 0
    const bottom = r.bottom ?? ((r.top ?? r.y ?? 0) + (r.height ?? 0))
    return { x: left, y: bottom }
  }
  return { x: props.x, y: props.y }
}

function adjustPosition() {
  if (!menuEl.value) return
  const rect = menuEl.value.getBoundingClientRect()
  const pad = 8
  const { x, y } = basePoint()
  let newX = x
  let newY = y
  if (newX + rect.width > window.innerWidth - pad) newX = Math.max(pad, window.innerWidth - rect.width - pad)
  if (newY + rect.height > window.innerHeight - pad) newY = Math.max(pad, window.innerHeight - rect.height - pad)
  posX.value = newX
  posY.value = newY
}

// Focusable rows in DOM order: menu items plus slider inputs.
function rows() {
  return menuEl.value ? [...menuEl.value.querySelectorAll('[data-menu-nav]:not([disabled])')] : []
}

function focusRow(i) {
  const list = rows()
  if (!list.length) return
  list[(i + list.length) % list.length]?.focus()
}

watch(() => props.modelValue, (open) => {
  if (open) {
    opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const p = basePoint()
    posX.value = p.x
    posY.value = p.y
    nextTick(() => {
      adjustPosition()
      // The container holds focus until an arrow key moves into the items.
      menuEl.value?.focus()
    })
  }
})

watch([() => props.x, () => props.y, () => props.anchor], () => {
  if (props.modelValue) {
    const p = basePoint()
    posX.value = p.x
    posY.value = p.y
    nextTick(adjustPosition)
  }
})

function restoreFocus() {
  const el = opener
  opener = null
  if (el && el.isConnected) el.focus?.()
}

function close({ focus = false } = {}) {
  emit('update:modelValue', false)
  emit('close')
  if (focus) nextTick(restoreFocus)
}

function handleSelect(item) {
  if (item.disabled) return
  if (item.action) item.action()
  emit('select', item)
  emit('update:modelValue', false)
  emit('close')
  // Return focus to the opener unless the action moved it somewhere else.
  nextTick(() => {
    const a = document.activeElement
    if (!a || a === document.body) restoreFocus()
    else opener = null
  })
}

function handleKeydown(e) {
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    close({ focus: true })
    return
  }
  if (e.key === 'Tab') {
    // Leave the menu; focus continues from the opener.
    close({ focus: true })
    return
  }

  const list = rows()
  if (!list.length) return
  const cur = list.indexOf(document.activeElement)

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    focusRow(cur < 0 ? 0 : cur + 1)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    focusRow(cur < 0 ? -1 : cur - 1)
  } else if (e.key === 'Home') {
    e.preventDefault()
    focusRow(0)
  } else if (e.key === 'End') {
    e.preventDefault()
    focusRow(-1)
  } else if (e.key.length === 1 && /\S/.test(e.key) && !e.ctrlKey && !e.metaKey && !e.altKey) {
    // Type-ahead: next row whose label starts with the typed letter.
    const ch = e.key.toLowerCase()
    const n = list.length
    for (let k = 1; k <= n; k++) {
      const el = list[(Math.max(cur, -1) + k) % n]
      if (el.textContent.trim().toLowerCase().startsWith(ch)) {
        el.focus()
        break
      }
    }
  }
}

function handleClickOutside(e) {
  if (props.modelValue && menuEl.value && !menuEl.value.contains(e.target)) {
    opener = null
    close()
  }
}

function handleScroll(e) {
  if (props.modelValue && !(menuEl.value && menuEl.value.contains(e.target))) {
    opener = null
    close()
  }
}

onMounted(() => {
  document.addEventListener('mousedown', handleClickOutside, true)
  document.addEventListener('contextmenu', handleClickOutside, true)
  window.addEventListener('scroll', handleScroll, true)
})

onUnmounted(() => {
  document.removeEventListener('mousedown', handleClickOutside, true)
  document.removeEventListener('contextmenu', handleClickOutside, true)
  window.removeEventListener('scroll', handleScroll, true)
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue"
      ref="menuEl"
      role="menu"
      :aria-label="ariaLabel || undefined"
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
          v-else-if="item.type === 'label'"
          role="presentation"
          class="px-2.5 pt-1.5 pb-1 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary truncate"
        >
          {{ item.label }}
        </div>

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
            data-menu-nav
            :aria-label="item.label"
            :aria-valuetext="`${item.value}%`"
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
          :role="item.type === 'radio' ? 'menuitemradio' : 'menuitem'"
          :aria-checked="item.type === 'radio' ? (item.checked ? 'true' : 'false') : undefined"
          :aria-disabled="item.disabled ? 'true' : undefined"
          data-menu-nav
          tabindex="-1"
          :disabled="item.disabled"
          :class="[
            'w-full h-8 px-2.5 flex items-center justify-between gap-3 rounded text-left transition-colors font-normal',
            item.disabled ? 'opacity-40 cursor-not-allowed' : 'cursor-pointer',
            item.danger
              ? 'text-red-400 hover:bg-red-500/10 hover:text-red-300'
              : 'hover:bg-mnema-hover hover:text-mnema-text',
            item.danger
              ? 'focus-visible:bg-red-500/10 focus-visible:text-red-300'
              : 'focus-visible:bg-mnema-hover focus-visible:text-mnema-text',
            'focus:outline-none'
          ]"
          @click="handleSelect(item)"
        >
          <div class="flex items-center gap-2 truncate">
            <component :is="item.icon" v-if="item.icon" class="w-4 h-4 flex-shrink-0 opacity-80" />
            <span class="truncate">{{ item.label }}</span>
          </div>

          <Check v-if="item.type === 'radio' && item.checked" class="w-4 h-4 flex-shrink-0 text-mnema-accent" aria-hidden="true" />
          <span v-else-if="item.shortcut" class="text-xs text-mnema-tertiary ml-auto font-mono flex-shrink-0">
            {{ item.shortcut }}
          </span>
        </button>
      </template>

      <slot name="footer"></slot>
    </div>
  </Teleport>
</template>
