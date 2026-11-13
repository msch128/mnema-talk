<script setup lang="ts">
// The confirm screen of a manifest: who the bot says it is (its own words,
// quoted and labelled as such), what it asks for (ticked, except what pings
// everyone or moderates; each can be changed), its commands and the channels it
// may work in (none ticked). Nothing is created until "Add bot"; an error keeps every tick.
import type { PropType } from 'vue'
import { ExternalLink, TriangleAlert } from '@lucide/vue'
import type { BotCheckedManifest } from '../../types/rest'
import { botInitial } from '../lib/bots'
import { homepageHost, INTENT_KEYS, OFF_AT_FIRST, safeHomepage, sharesBuiltinName } from '../lib/manifest'
import BotManifestChannels from './BotManifestChannels.vue'

const props = defineProps({
  manifest: { type: Object as PropType<BotCheckedManifest>, required: true },
  avatarUrl: { type: String, default: '' },
  avatarProblem: { type: String as PropType<'type' | 'size' | 'data' | null>, default: null },
  busy: { type: Boolean, default: false },
  error: { type: String, default: '' }
})
const intents = defineModel<string[]>('intents', { required: true })
const permissions = defineModel<string[]>('permissions', { required: true })
const channels = defineModel<string[]>('channels', { required: true })
const emit = defineEmits<{ back: []; submit: [] }>()
const homepage = () => safeHomepage(props.manifest.homepage)

function toggle(list: string[], value: string, on: boolean): string[] {
  return on ? [...list, value] : list.filter(x => x !== value)
}
</script>

<template>
  <form class="min-w-0 stack-6" data-testid="manifest-confirm" :aria-busy="busy ? 'true' : 'false'" @submit.prevent="emit('submit')">
    <figure class="min-w-0 rounded-lg border border-mnema-hairline bg-mnema-surface/60 p-3" data-testid="manifest-identity">
      <figcaption class="mb-2 text-[11px] font-medium uppercase tracking-wide text-mnema-tertiary">{{ $t('bots.manifest.fromManifest') }}</figcaption>
      <div class="flex min-w-0 items-start gap-3">
        <img v-if="avatarUrl" :src="avatarUrl" alt="" class="h-12 w-12 shrink-0 rounded-full object-cover" data-testid="manifest-avatar" />
        <span v-else class="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-mnema-raised text-lg font-semibold text-mnema-muted" aria-hidden="true">{{ botInitial(manifest.name) }}</span>
        <div class="min-w-0 flex-1 stack-1">
          <p class="flex min-w-0 items-center gap-1.5 text-sm text-mnema-text">
            <q class="min-w-0 truncate" :title="manifest.name" data-testid="manifest-name">{{ manifest.name }}</q>
            <span class="shrink-0 rounded-sm bg-mnema-accent/15 px-1 text-[10px] font-semibold uppercase text-mnema-accent">{{ $t('bots.badge') }}</span>
          </p>
          <blockquote v-if="manifest.description" class="border-l-2 border-mnema-border pl-2 text-sm whitespace-pre-wrap wrap-break-word text-mnema-muted">
            <q data-testid="manifest-description">{{ manifest.description }}</q>
          </blockquote>
          <p v-if="homepage()" class="flex min-w-0 flex-wrap items-center gap-x-1.5 text-xs text-mnema-tertiary">
            <span>{{ $t('bots.manifest.homepage') }}</span>
            <a :href="homepage()" target="_blank" rel="noopener noreferrer nofollow" class="inline-flex min-w-0 max-w-full items-center gap-1 text-mnema-accent underline underline-offset-2" :aria-label="$t('bots.manifest.homepageLink', { host: homepageHost(homepage()) })" data-testid="manifest-homepage">
              <span class="truncate" :title="homepage()">{{ homepage() }}</span>
              <ExternalLink class="h-3 w-3 shrink-0" aria-hidden="true" />
            </a>
            <span data-testid="manifest-homepage-external">{{ $t('bots.manifest.externalSite') }}</span>
          </p>
        </div>
      </div>
    </figure>
    <p v-if="avatarProblem" class="-mt-3 text-xs text-mnema-tertiary" data-testid="manifest-avatar-problem">{{ $t(`bots.manifest.avatarProblem.${avatarProblem}`) }}</p>

    <fieldset class="stack-2">
      <legend class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('bots.manifest.intentsTitle') }}</legend>
      <p v-if="!manifest.intents.length" class="text-sm text-mnema-tertiary">{{ $t('bots.manifest.noIntents') }}</p>
      <template v-else>
        <p class="text-sm text-mnema-tertiary">{{ $t('bots.manifest.untickHint') }}</p>
        <label v-for="intent in manifest.intents" :key="intent.name" class="flex items-start gap-2 text-sm text-mnema-text">
          <input type="checkbox" class="mt-0.5 shrink-0" :checked="intents.includes(intent.name)" :data-intent="intent.name" @change="intents = toggle(intents, intent.name, ($event.target as HTMLInputElement).checked)" />
          <span class="min-w-0">
            <span class="block">{{ $t(`bots.intents.${INTENT_KEYS[intent.name]}.name`) }}</span>
            <span class="block text-xs text-mnema-tertiary">{{ $t(`bots.intents.${INTENT_KEYS[intent.name]}.desc`) }}</span>
          </span>
        </label>
      </template>
    </fieldset>

    <fieldset class="stack-2">
      <legend class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('bots.manifest.permissionsTitle') }}</legend>
      <p v-if="!manifest.permissions.length" class="text-sm text-mnema-tertiary">{{ $t('bots.manifest.noPermissions') }}</p>
      <label v-for="perm in manifest.permissions" :key="perm.key" class="flex items-start gap-2 text-sm text-mnema-text">
        <input type="checkbox" class="mt-0.5 shrink-0" :checked="permissions.includes(perm.key)" :data-permission="perm.key" @change="permissions = toggle(permissions, perm.key, ($event.target as HTMLInputElement).checked)" />
        <span class="min-w-0">
          <span class="block">{{ $t(`perm.${perm.key}.name`) }}</span>
          <span class="block text-xs text-mnema-tertiary">{{ $t(`perm.${perm.key}.desc`) }}</span>
          <span v-if="OFF_AT_FIRST.has(perm.key)" class="block text-xs text-mnema-tertiary" :data-off-at-first="perm.key">{{ $t('bots.manifest.offAtFirst') }}</span>
        </span>
      </label>
    </fieldset>

    <section class="stack-2" :aria-label="$t('bots.manifest.commandsTitle')">
      <h5 class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('bots.manifest.commandsTitle') }}</h5>
      <p v-if="!manifest.commands.length" class="text-sm text-mnema-tertiary">{{ $t('bots.manifest.noCommands') }}</p>
      <ul v-else class="overflow-hidden rounded-lg border border-mnema-hairline" data-testid="manifest-commands">
        <li v-for="command in manifest.commands" :key="command.name" class="border-b border-mnema-hairline px-3 py-2 text-sm last:border-b-0">
          <span class="font-mono text-mnema-text">/{{ command.name }}</span>
          <span v-if="sharesBuiltinName(command.name)" class="ml-1.5 rounded-sm bg-mnema-raised px-1 text-[10px] font-semibold uppercase text-mnema-muted" :data-builtin-clash="command.name">{{ $t('bots.manifest.botCommand') }}</span>
          <span v-if="sharesBuiltinName(command.name)" class="block text-xs text-mnema-tertiary">{{ $t('bots.manifest.besideBuiltin', { name: command.name }) }}</span>
          <span v-if="command.description" class="block wrap-break-word text-xs text-mnema-tertiary">{{ command.description }}</span>
        </li>
      </ul>
    </section>

    <section class="stack-2" :aria-label="$t('bots.manifest.channelsTitle')">
      <h5 class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('bots.manifest.channelsTitle') }}</h5>
      <p class="text-sm text-mnema-tertiary">{{ $t('bots.manifest.channelsHint') }}</p>
      <p class="text-sm text-mnema-tertiary" data-testid="manifest-bot-role-note">{{ $t('bots.manifest.overridesBotRole') }}</p>
      <BotManifestChannels v-model="channels" />
    </section>

    <p v-if="error" role="alert" class="flex items-start gap-2 rounded-md border border-mnema-danger/40 bg-mnema-danger/10 px-3 py-2 text-sm text-mnema-text" data-testid="manifest-install-error">
      <TriangleAlert class="mt-0.5 h-4 w-4 shrink-0 text-mnema-danger" aria-hidden="true" />
      <span class="min-w-0 wrap-break-word">{{ error }}</span>
    </p>

    <div class="flex flex-wrap items-center justify-end gap-2 border-t border-mnema-hairline pt-4">
      <button type="button" class="btn btn-ghost" :disabled="busy" data-testid="manifest-back" @click="emit('back')">{{ $t('bots.manifest.back') }}</button>
      <button type="submit" class="btn btn-primary" :disabled="busy" :aria-busy="busy ? 'true' : undefined" data-testid="manifest-install">{{ $t('bots.manifest.install') }}</button>
    </div>
  </form>
</template>
