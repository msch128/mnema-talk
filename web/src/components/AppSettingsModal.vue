<script setup lang="ts">
import BaseDialog from './BaseDialog.vue'
import AppInstallSection from './AppInstallSection.vue'
import PushSection from './PushSection.vue'

const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <BaseDialog :title="$t('appSettings.title')" :subtitle="$t('appSettings.subtitle')" panel-class="max-w-lg select-none" @close="emit('close')">
    <div class="contents">
      <div class="p-6 space-y-6 overflow-y-auto max-h-[80vh]">
        <AppInstallSection />
        <PushSection />
      </div>
      <footer class="px-6 py-3.5 border-t border-mnema-hairline bg-mnema-raised flex justify-end">
        <button
          type="button"
          class="px-5 py-2 rounded-lg bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold text-sm transition shadow-xs"
          @click="emit('close')"
        >{{ $t('common.close') }}</button>
      </footer>
    </div>
  </BaseDialog>
</template>
