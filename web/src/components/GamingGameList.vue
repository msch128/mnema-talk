<script setup lang="ts">
// The extra game executables of the gaming settings: one row per name with a
// remove button, and an input where Enter (or Add) appends a validated entry.
import { ref, useId } from 'vue'
import { X } from '@lucide/vue'
import { checkGameName, type GameNameProblem } from '../lib/gameList'

const props = defineProps<{ modelValue: string[] }>()
const emit = defineEmits<{ 'update:modelValue': [next: string[]] }>()
const id = useId()
const example = 'Game.exe'
const draft = ref('')
const problem = ref<GameNameProblem | null>(null)

// Returns false when the typed text is not a valid entry (the problem is shown).
function add(): boolean {
  const result = checkGameName(draft.value, props.modelValue)
  if ('problem' in result) { problem.value = result.problem === 'empty' ? null : result.problem; return result.problem === 'empty' }
  emit('update:modelValue', [...props.modelValue, result.name])
  draft.value = ''; problem.value = null
  return true
}
// Save adds what is still typed, so it is not lost.
defineExpose({ add })
function remove(name: string) { emit('update:modelValue', props.modelValue.filter(item => item !== name)) }
</script>

<template>
  <div class="space-y-3">
    <ul v-if="modelValue.length" class="game-list" :aria-label="$t('gaming.games')">
      <li v-for="name in modelValue" :key="name" class="game-row">
        <span class="min-w-0 flex-1 truncate font-mono text-sm text-mnema-text" :title="name">{{ name }}</span>
        <button type="button" class="btn btn-icon btn-sm" :aria-label="$t('gaming.removeGame', { name })" @click="remove(name)"><X /></button>
      </li>
    </ul>
    <p v-else class="settings-hint">{{ $t('gaming.gamesEmpty') }}</p>
    <div class="flex items-start gap-2">
      <div class="min-w-0 flex-1">
        <input
          :id="`${id}-input`"
          v-model="draft"
          type="text"
          autocomplete="off"
          spellcheck="false"
          :placeholder="example"
          :aria-label="$t('gaming.addGame')"
          :aria-invalid="problem ? 'true' : undefined"
          :aria-describedby="problem ? `${id}-problem` : undefined"
          class="game-input w-full rounded-md border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text"
          @keydown.enter.prevent="add()"
          @input="problem = null"
        />
        <p v-if="problem" :id="`${id}-problem`" role="alert" class="field-error mt-1.5!">{{ $t(`gaming.problem.${problem}`, { max: 32 }) }}</p>
      </div>
      <button type="button" class="btn btn-secondary" data-testid="game-add" @click="add">{{ $t('gaming.addGame') }}</button>
    </div>
  </div>
</template>

<style scoped>
.game-list {display:flex;flex-direction:column;border:1px solid var(--color-mnema-hairline);border-radius:var(--radius-lg);max-height:14rem;overflow-y:auto;overflow-x:hidden}
.game-row {display:flex;align-items:center;gap:.75rem;min-height:2.75rem;padding:.25rem .5rem .25rem .875rem}
.game-row + .game-row {border-top:1px solid var(--color-mnema-hairline)}
</style>
