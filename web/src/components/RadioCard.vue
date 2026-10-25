<script setup lang="ts">
// One choice of an exclusive set: a card with a real radio, a title and what
// it means. Use for "pick one" settings; use SwitchRow for on/off.
defineOptions({ inheritAttrs: false })
defineProps<{ name: string; value: string; selected: boolean; title: string; description?: string; disabled?: boolean }>()
const emit = defineEmits<{ select: [value: string] }>()
</script>

<template>
  <label
    :class="[
      'flex cursor-pointer items-start gap-3 rounded-lg border px-4 py-3 transition-colors max-md:min-h-11 focus-within:ring-2 focus-within:ring-mnema-accent/50',
      selected ? 'border-mnema-accent/50 bg-mnema-accent/10' : 'border-mnema-border hover:bg-mnema-hover',
      disabled && 'cursor-not-allowed opacity-60'
    ]"
  >
    <input v-bind="$attrs" type="radio" :name="name" :value="value" :checked="selected" :disabled="disabled" class="mt-1" @change="emit('select', value)" />
    <span class="min-w-0">
      <span class="block text-sm font-medium text-mnema-text">{{ title }}</span>
      <span v-if="description" class="block text-xs text-mnema-tertiary">{{ description }}</span>
    </span>
  </label>
</template>
