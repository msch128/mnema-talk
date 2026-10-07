<script setup lang="ts">
import type { PropType } from 'vue'
import type { ContextMenuItem } from './menuTypes'
// One row of a ContextMenu (or of one of its submenus); see ContextMenu.vue
// for the item shapes. Submenu triggers are rendered by the menu itself.
import { Check } from '@lucide/vue'

defineProps({ item: { type: Object as PropType<ContextMenuItem>, required: true } })
const emit = defineEmits<{ select: [item: ContextMenuItem] }>()

function role(item: ContextMenuItem) {
  if (item.type === 'radio') return 'menuitemradio'
  if (item.type === 'checkbox') return 'menuitemcheckbox'
  return 'menuitem'
}
</script>

<template>
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
      @input="item.onInput?.(Number(($event.target as HTMLInputElement).value))"
      class="w-full cursor-pointer accent-mnema-accent"
    />
  </div>

  <!-- Read-only line (label: value), focusable so screen readers reach it -->
  <div
    v-else-if="item.type === 'info'"
    role="menuitem"
    aria-disabled="true"
    data-menu-nav
    tabindex="-1"
    :data-menu-info="item.id || undefined"
    class="w-full h-7 px-2.5 flex items-center justify-between gap-4 rounded text-left font-normal focus:outline-none focus-visible:bg-mnema-hover"
  >
    <span class="truncate text-mnema-muted">{{ item.label }}</span>
    <span class="font-mono text-xs text-mnema-text flex-shrink-0">{{ item.value }}</span>
  </div>

  <button
    v-else
    type="button"
    :role="role(item)"
    :aria-checked="item.type === 'radio' || item.type === 'checkbox' ? (item.checked ? 'true' : 'false') : undefined"
    :aria-disabled="item.disabled ? 'true' : undefined"
    :data-menu-item="item.id || undefined"
    data-menu-nav
    tabindex="-1"
    :disabled="item.disabled"
    :class="[
      'w-full px-2.5 flex items-center justify-between gap-3 rounded text-left transition-colors font-normal',
      item.subtitle ? 'min-h-8 py-1.5' : 'h-8',
      item.disabled ? 'opacity-40 cursor-not-allowed' : 'cursor-pointer',
      item.danger
        ? 'text-red-400 hover:bg-red-500/10 hover:text-red-300'
        : 'hover:bg-mnema-hover hover:text-mnema-text',
      item.danger
        ? 'focus-visible:bg-red-500/10 focus-visible:text-red-300'
        : 'focus-visible:bg-mnema-hover focus-visible:text-mnema-text',
      'focus:outline-none'
    ]"
    @click="emit('select', item)"
  >
    <div class="flex items-center gap-2 min-w-0">
      <component :is="item.icon" v-if="item.icon" class="w-4 h-4 flex-shrink-0 opacity-80" />
      <span class="flex flex-col min-w-0">
        <span class="truncate">{{ item.label }}</span>
        <span v-if="item.subtitle" class="truncate text-xs text-mnema-tertiary">{{ item.subtitle }}</span>
      </span>
    </div>

    <Check v-if="item.type === 'radio' && item.checked" class="w-4 h-4 flex-shrink-0 text-mnema-accent" aria-hidden="true" />
    <span
      v-else-if="item.type === 'checkbox'"
      aria-hidden="true"
      :class="[
        'w-4 h-4 flex-shrink-0 rounded-[4px] flex items-center justify-center border',
        item.checked ? 'bg-mnema-accent border-mnema-accent text-mnema-accent-ink' : 'border-mnema-border-strong'
      ]"
    >
      <Check v-if="item.checked" class="w-3 h-3" />
    </span>
    <span v-else-if="item.shortcut" class="text-xs text-mnema-tertiary ml-auto font-mono flex-shrink-0">
      {{ item.shortcut }}
    </span>
  </button>
</template>
