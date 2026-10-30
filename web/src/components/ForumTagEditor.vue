<script setup lang="ts">
import { ref, computed } from 'vue'
import { Check, Loader2 } from '@lucide/vue'
import { charCount } from '../lib/textLength'
import type { ForumTag } from '../types/domain'
import { TAG_COLORS, tagSwatchClass } from '../lib/forumTags'
import ForumTagChip from './ForumTagChip.vue'
import SwitchRow from './SwitchRow.vue'

// The form of one tag: name, emoji, colour and whether only moderators apply it.
// Used for a new tag (no `tag`) and for editing one.
const props = defineProps<{
  tag?: ForumTag
  saving: boolean
}>()

const emit = defineEmits<{
  save: [fields: { name: string; emoji: string; color: string; moderated: boolean }]
  cancel: []
}>()

const name = ref(props.tag?.name ?? '')
const emoji = ref(props.tag?.emoji ?? '')
const color = ref(props.tag?.color ?? '')
const moderated = ref(props.tag?.moderated ?? false)

const trimmedName = computed(() => name.value.trim())
const valid = computed(() => trimmedName.value.length > 0 && charCount(trimmedName.value) <= 20)
const preview = computed<ForumTag>(() => ({
  id: 'preview', channel_id: '', position: 0, created_at: '',
  name: trimmedName.value || '…', emoji: emoji.value.trim(), moderated: moderated.value,
  ...(color.value ? { color: color.value as NonNullable<ForumTag['color']> } : {})
}))

function submit() {
  if (!valid.value || props.saving) return
  emit('save', { name: trimmedName.value, emoji: emoji.value.trim(), color: color.value, moderated: moderated.value })
}
</script>

<template>
  <form class="space-y-3 rounded-lg border border-mnema-hairline bg-mnema-surface/60 p-3" data-testid="forum-tag-editor" @submit.prevent="submit" @keydown.esc.stop="emit('cancel')">
    <div class="flex items-center gap-2">
      <input
        v-model="emoji"
        type="text"
        v-max-chars="32"
        :aria-label="$t('forum.emojiPlaceholder')"
        :placeholder="$t('forum.emojiPlaceholder')"
        class="w-16 rounded-lg border border-mnema-border bg-mnema-surface px-2 py-1.5 text-center text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent"
      />
      <input
        v-model="name"
        data-testid="forum-tag-name"
        type="text"
        v-max-chars="20"
        :aria-label="$t('forum.tagNamePlaceholder')"
        :placeholder="$t('forum.tagNamePlaceholder')"
        class="min-w-0 flex-1 rounded-lg border border-mnema-border bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent"
      />
    </div>

    <div role="radiogroup" :aria-label="$t('forum.tagColor')" class="flex flex-wrap items-center gap-2">
      <button
        type="button"
        role="radio"
        :aria-checked="color === '' ? 'true' : 'false'"
        :aria-label="$t('forum.colorNone')"
        :class="['flex h-6 w-6 items-center justify-center rounded-full border border-mnema-border-strong text-mnema-tertiary transition', color === '' ? 'ring-2 ring-mnema-accent' : 'hover:border-mnema-text']"
        @click="color = ''"
      >
        <Check v-if="color === ''" class="h-3 w-3" />
      </button>
      <button
        v-for="c in TAG_COLORS"
        :key="c"
        type="button"
        role="radio"
        :aria-checked="color === c ? 'true' : 'false'"
        :aria-label="$t(`forum.color.${c}`)"
        v-tooltip="$t(`forum.color.${c}`)"
        :class="['flex h-6 w-6 items-center justify-center rounded-full text-black/70 transition', tagSwatchClass(c), color === c ? 'ring-2 ring-mnema-accent ring-offset-2 ring-offset-mnema-elevated' : 'opacity-80 hover:opacity-100']"
        @click="color = c"
      >
        <Check v-if="color === c" class="h-3 w-3" />
      </button>
    </div>

    <SwitchRow
      v-model="moderated"
      data-testid="forum-tag-moderated"
      :title="$t('forum.moderatedTitle')"
      :hint="$t('forum.moderatedHint')"
    />

    <div class="flex items-center justify-between gap-3 pt-1">
      <ForumTagChip :tag="preview" />
      <div class="flex items-center gap-2">
        <button type="button" class="btn btn-secondary px-3 py-1.5 text-xs" @click="emit('cancel')">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="!valid || saving" data-testid="forum-tag-save" class="btn btn-primary flex items-center gap-1.5 px-3 py-1.5 text-xs">
          <Loader2 v-if="saving" class="h-3.5 w-3.5 animate-spin" />
          <span>{{ $t('common.save') }}</span>
        </button>
      </div>
    </div>
  </form>
</template>
