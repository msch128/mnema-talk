<script setup lang="ts">
// The screen while the server does not answer at start-up: it keeps trying by
// itself, and says when it last did. The sentence about the last try changes
// at most once a minute so a screen reader is not read a ticking value.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { locale, t } from '../../i18n'
import { relative } from '../../shared/lib/time'

const props = defineProps<{ trying: boolean; lastTriedAt: number | null }>()
defineEmits<{ retry: [] }>()

const now = ref(Date.now())
let ticker: ReturnType<typeof setInterval> | undefined
onMounted(() => { ticker = setInterval(() => { now.value = Date.now() }, 15000) })
onBeforeUnmount(() => clearInterval(ticker))

const lastTried = computed(() => {
  if (props.lastTriedAt === null) return ''
  const ago = now.value - props.lastTriedAt
  const when = ago < 60000 ? t('app.lastTriedNow') : relative(props.lastTriedAt, locale.value, new Date(now.value))
  return t('app.lastTried', { when })
})
</script>

<template>
  <div class="flex-1 flex flex-col items-center justify-center gap-3" data-testid="server-unreachable">
    <div class="w-10 h-10 rounded-lg bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-semibold text-base animate-pulse">
      M
    </div>
    <div role="status" aria-live="polite" class="flex flex-col items-center gap-1 text-center">
      <span class="text-sm text-mnema-tertiary">{{ $t('app.retrying') }}</span>
      <span v-if="lastTried" class="text-xs text-mnema-tertiary" data-testid="last-tried">{{ lastTried }}</span>
    </div>
    <button type="button" class="btn btn-secondary btn-sm" :disabled="trying" data-testid="try-again" @click="$emit('retry')">
      {{ $t(trying ? 'app.trying' : 'app.tryAgain') }}
    </button>
  </div>
</template>
