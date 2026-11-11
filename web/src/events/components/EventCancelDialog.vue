<script setup lang="ts">
import { ref } from 'vue'
import BaseDialog from '../../shared/components/BaseDialog.vue'
import ScopeChoice from './ScopeChoice.vue'
import type { Scope } from '../lib/eventRecurrence'

// Cancelling an occurrence of a repeating event: only this one, or it and all later ones (the series then ends).
const emit = defineEmits<{ close: []; confirm: [scope: Scope] }>()
const scope = ref<Scope>('this')
</script>

<template>
  <BaseDialog :title="$t('events.cancel')" panel-class="max-w-md" @close="emit('close')">
    <form class="flex flex-col gap-4 p-6 max-md:p-4" @submit.prevent="emit('confirm', scope)">
      <p class="text-sm text-mnema-secondary">{{ $t('events.cancelConfirm') }}</p>
      <ScopeChoice v-model="scope" :hint="scope === 'following' ? $t('events.scope.cancelHint') : ''" />
      <div class="flex justify-end gap-3 border-t border-mnema-hairline pt-4">
        <button type="button" class="rounded-lg border border-mnema-border px-4 py-2 text-sm font-medium text-mnema-text hover:bg-mnema-hover max-md:min-h-11" @click="emit('close')">{{ $t('events.cancelKeep') }}</button>
        <button type="submit" data-testid="cancel-confirm" class="rounded-lg bg-red-500 px-4 py-2 text-sm font-medium text-white hover:opacity-90 max-md:min-h-11">{{ $t('events.cancel') }}</button>
      </div>
    </form>
  </BaseDialog>
</template>
