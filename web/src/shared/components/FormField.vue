<script setup lang="ts">
// A labelled form control: a visible label above, an optional hint and an
// error line below, wired to the control through the slot props so screen
// readers read all of them. Never use a placeholder as the label.
import { computed, useId } from 'vue'

// inputId pins a stable id (for links and tests); otherwise one is generated.
const props = defineProps<{ label: string; hint?: string; error?: string; inputId?: string }>()

const generated = useId()
const id = props.inputId ?? generated
const hintId = `${id}-hint`
const errorId = `${id}-error`
const describedby = computed(() => [props.hint && hintId, props.error && errorId].filter(Boolean).join(' ') || undefined)
</script>

<template>
  <div class="space-y-1.5">
    <label :for="id" class="block text-sm font-medium text-mnema-body-ink">{{ label }}</label>
    <slot :id="id" :describedby="describedby" :invalid="!!error" />
    <p v-if="hint" :id="hintId" class="field-hint mt-1!">{{ hint }}</p>
    <p v-if="error" :id="errorId" role="alert" class="field-error mt-1!">{{ error }}</p>
  </div>
</template>
