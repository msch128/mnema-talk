<script setup lang="ts">
// A password field with a show/hide toggle. Attributes (id, autocomplete,
// required, data-testid, aria-describedby ...) land on the input itself.
import { ref } from 'vue'
import { Eye, EyeOff } from '@lucide/vue'

defineOptions({ inheritAttrs: false })
const model = defineModel<string>({ default: '' })
const el = ref<HTMLInputElement | null>(null)
const visible = ref(false)
defineExpose({ el })
</script>

<template>
  <div class="relative">
    <input
      v-bind="$attrs"
      ref="el"
      v-model="model"
      :type="visible ? 'text' : 'password'"
      autocapitalize="none"
      spellcheck="false"
      class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas py-2 pl-3 pr-11 text-sm text-mnema-text"
    />
    <button
      type="button"
      class="absolute right-1 top-1/2 flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-md text-mnema-tertiary transition-colors hover:text-mnema-text focus:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent max-md:h-10 max-md:w-10"
      :aria-label="visible ? $t('security.hidePassword') : $t('security.showPassword')"
      :aria-pressed="visible ? 'true' : 'false'"
      data-testid="password-toggle"
      @click="visible = !visible"
    >
      <EyeOff v-if="visible" class="h-4 w-4" aria-hidden="true" />
      <Eye v-else class="h-4 w-4" aria-hidden="true" />
    </button>
  </div>
</template>
