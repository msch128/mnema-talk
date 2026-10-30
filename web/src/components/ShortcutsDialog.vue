<script setup lang="ts">
// The keyboard shortcuts of the app, read only, grouped by where they work.
// The list is lib/shortcuts; the push-to-talk key is the member's own.
import { computed } from 'vue'
import BaseDialog from './BaseDialog.vue'
import { SHORTCUT_GROUPS, isMac, keyLabel, type KeyCombo, type Shortcut } from '../lib/shortcuts'
import { pttKeyLabel } from '../lib/pttKey'
import { useVoiceStore } from '../stores/voice'

const emit = defineEmits<{ close: [] }>()
const voice = useVoiceStore()
const mac = isMac()

const pttCombos = computed<readonly KeyCombo[]>(() => (voice.pttKey ? [[pttKeyLabel(voice.pttKey)]] : []))
const combosOf = (item: Shortcut): readonly KeyCombo[] => (item.id === 'ptt' ? pttCombos.value : item.combos)
</script>

<template>
  <BaseDialog :title="$t('shortcuts.title')" :subtitle="$t('shortcuts.subtitle')" panel-class="max-w-3xl" sheet @close="emit('close')">
    <div class="min-h-0 flex-1 overflow-y-auto p-6 max-md:p-4" data-testid="shortcuts-dialog">
      <div class="grid gap-x-10 gap-y-7 md:grid-cols-2">
        <section v-for="group in SHORTCUT_GROUPS" :key="group.id" :aria-labelledby="`shortcuts-${group.id}`" :data-testid="`shortcuts-group-${group.id}`">
          <h3 :id="`shortcuts-${group.id}`" class="settings-label">{{ $t(`shortcuts.group.${group.id}`) }}</h3>
          <p v-if="group.hint" class="settings-hint mb-1.5">{{ $t(`shortcuts.groupHint.${group.id}`) }}</p>
          <ul class="divide-y divide-mnema-hairline">
            <li v-for="item in group.items" :key="item.id" class="flex items-start justify-between gap-4 py-2" :data-shortcut="item.id">
              <span class="min-w-0 text-sm text-mnema-body-ink">{{ $t(`shortcuts.${group.id}.${item.id}`) }}</span>
              <span class="flex shrink-0 flex-wrap items-center justify-end gap-x-1.5 gap-y-1 text-xs text-mnema-tertiary">
                <template v-for="(combo, ci) in combosOf(item)" :key="ci">
                  <span v-if="ci > 0">{{ $t('shortcuts.or') }}</span>
                  <span class="inline-flex items-center gap-1">
                    <kbd
                      v-for="(key, ki) in combo"
                      :key="ki"
                      class="rounded-md border border-mnema-border bg-mnema-raised px-1.5 py-0.5 font-mono text-xs text-mnema-text"
                    >{{ keyLabel(key, mac) }}</kbd>
                  </span>
                </template>
                <span v-if="!combosOf(item).length" class="italic">{{ $t('shortcuts.notSet') }}</span>
              </span>
            </li>
          </ul>
        </section>
      </div>
    </div>
  </BaseDialog>
</template>
