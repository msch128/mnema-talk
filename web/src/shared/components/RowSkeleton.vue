<script setup lang="ts">
// Placeholder rows while a list loads for the first time (a channel being
// opened, search results, forum posts): the shape of what is coming instead of
// an empty area or a lone spinner. The rows breathe slowly and stand still
// with reduced motion; screen readers hear one "Loading…".
defineProps({
  rows: { type: Number, default: 6 },
  // Message rows: a 40px avatar beside a name and a line of text.
  // Compact rows: a 32px avatar (search results). Cards: no avatar, a title and two lines (forum posts).
  shape: { type: String as () => 'message' | 'compact' | 'card', default: 'message' },
  // What screen readers hear; "Loading…" unless the list says more.
  label: { type: String, default: '' }
})

// Varied line lengths read as text rather than as a grid of bars.
const WIDTHS = ['w-5/6', 'w-2/3', 'w-3/4', 'w-1/2', 'w-4/5', 'w-3/5']
const widthOf = (n: number) => WIDTHS[n % WIDTHS.length]
</script>

<template>
  <div role="status" data-testid="row-skeleton">
    <span class="sr-only">{{ label || $t('common.loading') }}</span>
    <div aria-hidden="true" :class="shape === 'card' ? 'stack-3' : shape === 'compact' ? 'stack-3 px-2 py-3' : 'stack-5 px-4 py-2'">
      <div
        v-for="n in rows"
        :key="n"
        :class="[
          'flex animate-pulse items-start motion-reduce:animate-none',
          shape === 'card' ? 'flex-col gap-2 rounded-xl border border-mnema-hairline bg-mnema-raised p-4' : shape === 'compact' ? 'gap-3 px-1' : 'gap-4'
        ]"
      >
        <div v-if="shape !== 'card'" :class="['shrink-0 rounded-full bg-mnema-hover', shape === 'compact' ? 'h-8 w-8' : 'h-10 w-10']"></div>
        <div :class="shape === 'card' ? 'w-full stack-2' : 'min-w-0 flex-1 stack-2 pt-1'">
          <div :class="['h-3 rounded-sm bg-mnema-hover', shape === 'card' ? 'w-1/2' : 'w-1/4']"></div>
          <div :class="['h-3 rounded-sm bg-mnema-hover', widthOf(n)]"></div>
          <div v-if="shape === 'card'" :class="['h-3 rounded-sm bg-mnema-hover', widthOf(n + 2)]"></div>
        </div>
      </div>
    </div>
  </div>
</template>
