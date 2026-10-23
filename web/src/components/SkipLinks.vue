<script setup lang="ts">
// The first two Tab stops of the app. They are hidden until focused and jump past
// the channel list: to the messages of the open channel (or the Talk) and to the
// message box. The Tab order after them follows the layout: channel list, header,
// messages, message box, then the member list.
function focusMain() {
  const main = document.getElementById('main-content')
  main?.focus()
  main?.scrollIntoView?.({ block: 'nearest' })
}

function focusComposer() {
  document.querySelector<HTMLElement>('[data-composer-input]')?.focus()
}

const link = 'rounded-md bg-mnema-accent px-3 py-2 text-sm font-semibold text-mnema-accent-ink shadow-lg outline-none focus-visible:ring-2 focus-visible:ring-mnema-accent focus-visible:ring-offset-2 focus-visible:ring-offset-mnema-canvas'
</script>

<template>
  <div
    data-testid="skip-links"
    class="sr-only focus-within:not-sr-only focus-within:fixed focus-within:left-2 focus-within:top-2 focus-within:z-[100] focus-within:flex focus-within:gap-2"
  >
    <button type="button" data-testid="skip-to-main" :class="link" @click="focusMain">{{ $t('skip.toMessages') }}</button>
    <button type="button" data-testid="skip-to-composer" :class="link" @click="focusComposer">{{ $t('skip.toComposer') }}</button>
  </div>
</template>
