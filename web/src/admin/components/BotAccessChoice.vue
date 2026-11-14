<script setup lang="ts">
// Where a bot may work (doc/bots.md, "Give a bot access to every channel"):
// the ticked channels only, or every channel. With "all" the channel list is
// for the private channels the bot may also see. Used when adding a bot and in
// its settings; the server checks the choice again.
import { useId } from 'vue'
import BotManifestChannels from './BotManifestChannels.vue'

export type AccessMode = 'channels' | 'all'

defineProps({
  disabled: { type: Boolean, default: false },
  /** The role note only fits where a manifest's permissions are ticked. */
  roleNote: { type: Boolean, default: false }
})
const mode = defineModel<AccessMode>('mode', { required: true })
const channels = defineModel<string[]>('channels', { required: true })
const group = `access-${useId()}`
const OPTIONS: readonly AccessMode[] = ['channels', 'all']
</script>

<template>
  <fieldset class="min-w-0 stack-3" data-testid="bot-access-choice" :disabled="disabled">
    <legend class="mb-1 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('bots.choice.title') }}</legend>
    <label v-for="option in OPTIONS" :key="option" class="flex min-w-0 items-start gap-2 rounded-md px-1 py-1 text-sm text-mnema-text hover:bg-mnema-hover">
      <input v-model="mode" type="radio" class="mt-0.5 shrink-0" :name="group" :value="option" :data-access="option" />
      <span class="min-w-0">
        <span class="block">{{ $t(`bots.choice.${option}.name`) }}</span>
        <span class="block text-xs text-mnema-tertiary">{{ $t(`bots.choice.${option}.desc`) }}</span>
      </span>
    </label>

    <template v-if="mode === 'all'">
      <p class="text-sm text-mnema-tertiary" data-testid="bot-access-private-note">{{ $t('bots.choice.privateNote') }}</p>
      <details class="min-w-0 rounded-lg border border-mnema-hairline px-3 py-2" data-testid="bot-access-private">
        <summary class="cursor-pointer text-sm text-mnema-text">{{ $t('bots.choice.privateTitle') }}</summary>
        <div class="mt-2 stack-3">
          <p class="text-xs text-mnema-tertiary">{{ $t('bots.choice.privateHint') }}</p>
          <BotManifestChannels v-model="channels" />
        </div>
      </details>
    </template>
    <template v-else>
      <p class="text-sm text-mnema-tertiary">{{ $t('bots.manifest.channelsHint') }}</p>
      <p v-if="roleNote" class="text-sm text-mnema-tertiary" data-testid="manifest-bot-role-note">{{ $t('bots.manifest.overridesBotRole') }}</p>
      <BotManifestChannels v-model="channels" />
    </template>
  </fieldset>
</template>
