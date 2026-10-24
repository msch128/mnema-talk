<script setup lang="ts">
// Admin dashboard: the instance's custom emoji (upload, rename, delete).
import { ref, computed, onMounted } from 'vue'
import { Smile, Trash2, Pencil, Check, X } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { confirm } from '../lib/confirm'
import { emojiImage } from '../lib/customEmoji'
import { useEmojiStore, prefersReducedMotion } from '../stores/emoji'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

const store = useEmojiStore()
const toasts = useToastStore()

const name = ref('')
const file = ref<File | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const busy = ref(false)
const editing = ref<string | null>(null)
const draft = ref('')

const NAME_RE = /^[a-z0-9_]{2,32}$/
const nameOk = computed(() => NAME_RE.test(name.value.trim().toLowerCase()))
const full = computed(() => store.items.length >= store.limit)

function showError(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}

function onFile(event: Event) {
  const picked = (event.target as HTMLInputElement).files?.[0] ?? null
  file.value = picked
  // The file name proposes a name; the admin can change it.
  if (picked && !name.value) {
    const base = picked.name.replace(/\.[^.]+$/, '').toLowerCase().replace(/[^a-z0-9_]+/g, '_').slice(0, 32)
    if (NAME_RE.test(base)) name.value = base
  }
}

async function upload() {
  if (!file.value || !nameOk.value || busy.value) return
  busy.value = true
  try {
    await store.upload(name.value.trim().toLowerCase(), file.value)
    toasts.success(t('emoji.admin.added'))
    name.value = ''
    file.value = null
    if (fileInput.value) fileInput.value.value = ''
  } catch (e) {
    showError(e)
  } finally {
    busy.value = false
  }
}

function startRename(id: string, current: string) {
  editing.value = id
  draft.value = current
}

async function saveRename(id: string) {
  const next = draft.value.trim().toLowerCase()
  if (!NAME_RE.test(next)) return
  try {
    await store.rename(id, next)
    editing.value = null
  } catch (e) {
    showError(e)
  }
}

async function remove(id: string, emojiName: string) {
  const ok = await confirm({
    title: t('emoji.admin.deleteTitle', { name: emojiName }),
    body: t('emoji.admin.deleteBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  try {
    await store.remove(id)
    toasts.success(t('emoji.admin.deleted'))
  } catch (e) {
    showError(e)
  }
}

onMounted(() => { void store.load() })
</script>

<template>
  <section class="space-y-4">
    <div class="flex items-center justify-between">
      <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider flex items-center gap-2">
        <Smile class="w-4 h-4 text-mnema-accent" />
        <span>{{ $t('emoji.admin.title') }}</span>
      </h3>
      <span class="text-xs text-mnema-tertiary" data-testid="emoji-count">{{ $t('emoji.admin.count', { n: store.items.length, max: store.limit }) }}</span>
    </div>
    <p class="text-sm text-mnema-muted">{{ $t('emoji.admin.hint') }}</p>

    <form class="flex flex-wrap gap-2" @submit.prevent="upload">
      <!-- The browser's own file control is unstyled and in the browser's language: a button and the chosen name instead. -->
      <div class="flex min-w-0 flex-1 basis-56 items-center gap-3">
        <input
          ref="fileInput"
          type="file"
          accept="image/png,image/gif,image/webp"
          data-testid="emoji-file"
          :aria-label="$t('emoji.admin.file')"
          :disabled="full"
          tabindex="-1"
          class="sr-only"
          @change="onFile"
        />
        <button
          type="button"
          :disabled="full"
          data-testid="emoji-choose"
          class="btn btn-secondary shrink-0"
          @click="fileInput?.click()"
        >{{ $t('emoji.admin.choose') }}</button>
        <span class="min-w-0 truncate text-sm text-mnema-muted" data-testid="emoji-file-name">{{ file?.name ?? $t('emoji.admin.noFile') }}</span>
      </div>
      <input
        v-model="name"
        type="text"
        maxlength="32"
        data-testid="emoji-name"
        :placeholder="$t('emoji.admin.name')"
        :aria-label="$t('emoji.admin.name')"
        :disabled="full"
        class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-hidden min-w-0 flex-1 basis-40 focus:border-mnema-accent transition"
      />
      <button
        type="submit"
        data-testid="emoji-upload"
        :disabled="!file || !nameOk || busy || full"
        class="btn btn-primary"
      >
        {{ $t('emoji.admin.add') }}
      </button>
    </form>
    <p v-if="full" class="text-xs text-mnema-danger">{{ $t('emoji.admin.full') }}</p>

    <div class="bg-mnema-surface rounded-lg border border-mnema-hairline overflow-hidden divide-y divide-mnema-hairline">
      <div v-if="!store.items.length" class="p-4 text-sm text-mnema-tertiary">{{ $t('emoji.admin.empty') }}</div>
      <div v-for="e in store.items" :key="e.id" class="px-4 py-2 flex items-center justify-between gap-3 text-sm" data-testid="emoji-row">
        <div class="flex items-center gap-3 min-w-0">
          <img :src="emojiImage(e, prefersReducedMotion()).src" :alt="`:${e.name}:`" class="h-8 w-8 object-contain shrink-0" />
          <form v-if="editing === e.id" class="flex items-center gap-1.5" @submit.prevent="saveRename(e.id)">
            <input
              v-model="draft"
              type="text"
              maxlength="32"
              :aria-label="$t('emoji.admin.name')"
              class="bg-mnema-elevated border border-mnema-border-field rounded-md px-2 py-1 text-sm text-mnema-text outline-hidden focus:border-mnema-accent"
            />
            <button type="submit" class="text-mnema-accent" :aria-label="$t('common.save')"><Check class="w-4 h-4" /></button>
            <button type="button" class="text-mnema-tertiary" :aria-label="$t('common.cancel')" @click="editing = null"><X class="w-4 h-4" /></button>
          </form>
          <span v-else class="font-mono text-mnema-text truncate">:{{ e.name }}:</span>
          <span v-if="e.animated" class="text-xs text-mnema-tertiary">{{ $t('emoji.admin.animated') }}</span>
        </div>
        <div v-if="editing !== e.id" class="flex items-center gap-3 shrink-0">
          <button type="button" class="text-mnema-tertiary hover:text-mnema-accent transition" :title="$t('emoji.admin.rename')" :aria-label="$t('emoji.admin.rename')" @click="startRename(e.id, e.name)">
            <Pencil class="w-4 h-4" />
          </button>
          <button type="button" class="text-mnema-tertiary hover:text-mnema-danger transition" :title="$t('common.delete')" :aria-label="$t('common.delete')" @click="remove(e.id, e.name)">
            <Trash2 class="w-4 h-4" />
          </button>
        </div>
      </div>
    </div>
  </section>
</template>
