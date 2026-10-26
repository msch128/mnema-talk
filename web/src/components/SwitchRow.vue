<script setup lang="ts">
// An on/off setting: title and explanation left, the switch right. Attributes
// (data-testid ...) land on the switch button.
import { useId } from 'vue'

defineOptions({ inheritAttrs: false })
// Controlled: the parent owns the state and answers the toggle event.
const props = defineProps<{ modelValue: boolean; title: string; hint?: string; hintTestid?: string; disabled?: boolean }>()
const emit = defineEmits<{ toggle: [next: boolean]; 'update:modelValue': [next: boolean] }>()
const id = useId()

function flip() {
  emit('toggle', !props.modelValue)
  emit('update:modelValue', !props.modelValue)
}
</script>

<template>
  <div class="flex items-start justify-between gap-4">
    <div class="min-w-0 space-y-0.5">
      <div :id="`${id}-title`" class="text-sm font-semibold text-mnema-text">{{ title }}</div>
      <p v-if="hint" :id="`${id}-hint`" :data-testid="hintTestid" class="text-xs leading-relaxed text-mnema-tertiary">{{ hint }}</p>
    </div>
    <button
      v-bind="$attrs"
      type="button"
      role="switch"
      :aria-checked="modelValue ? 'true' : 'false'"
      :aria-labelledby="`${id}-title`"
      :aria-describedby="hint ? `${id}-hint` : undefined"
      :disabled="disabled"
      class="switch mt-0.5"
      @click="flip"
    ></button>
  </div>
</template>
