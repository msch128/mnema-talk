<script setup lang="ts">
import { computed } from 'vue'
import { Check } from '@lucide/vue'
import { charCount } from '../../shared/lib/textLength'
import type { ForumTag } from '../../types/domain'
import { TAG_COLORS, tagSwatchClass } from '../lib/forumTags'
import { normalise, useSettingsDraft } from '../../shared/composables/useSettingsDraft'
import type { FieldErrors } from '../../shared/composables/useSettingsDraft'
import { t } from '../../i18n'
import ForumTagChip from './ForumTagChip.vue'
import SwitchRow from '../../shared/components/SwitchRow.vue'
import UnsavedChangesBar from '../../shared/components/UnsavedChangesBar.vue'

// The form of one tag: name, emoji, colour and whether only moderators apply it.
// Used for a new tag (no `tag`) and for editing one. It is a draft with its own
// bar inside the card; the parent does the writing and throws when it fails.
type Fields = { name: string; emoji: string; color: string; moderated: boolean }
const props = defineProps<{
  tag?: ForumTag
  save: (fields: Fields) => Promise<void>
}>()

const emit = defineEmits<{ cancel: [] }>()

const form = useSettingsDraft<Fields>({
  source: () => ({ name: props.tag?.name ?? '', emoji: props.tag?.emoji ?? '', color: props.tag?.color ?? '', moderated: props.tag?.moderated ?? false }),
  normalisers: { name: normalise.text, emoji: normalise.text, moderated: normalise.flag },
  validate: d => {
    const found: FieldErrors<Fields> = {}
    const name = d.name.trim()
    if (name.length === 0 || charCount(name) > 20) found.name = t('forum.tagNamePlaceholder')
    return found
  },
  save: ({ draft }) => props.save({ name: draft.name.trim(), emoji: draft.emoji.trim(), color: draft.color, moderated: draft.moderated })
})
const { draft } = form

const preview = computed<ForumTag>(() => ({
  id: 'preview', channel_id: '', position: 0, created_at: '',
  name: draft.name.trim() || '…', emoji: draft.emoji.trim(), moderated: draft.moderated,
  ...(draft.color ? { color: draft.color as NonNullable<ForumTag['color']> } : {})
}))

/** Leaving the editor (Cancel, Escape) asks first while it holds unsaved input. */
async function cancel() {
  if (await form.guard()) emit('cancel')
}

defineExpose({ guard: form.guard })
</script>

<template>
  <form class="space-y-3 rounded-lg border border-mnema-hairline bg-mnema-surface/60 p-3" data-testid="forum-tag-editor" @submit.prevent="form.save" @keydown="form.onKeydown" @keydown.esc.stop="cancel">
    <div class="flex items-center gap-2">
      <input
        v-model="draft.emoji"
        type="text"
        v-max-chars="32"
        :aria-label="$t('forum.emojiPlaceholder')"
        :placeholder="$t('forum.emojiPlaceholder')"
        class="w-16 rounded-lg border border-mnema-border bg-mnema-surface px-2 py-1.5 text-center text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent"
      />
      <input
        v-model="draft.name"
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
        :aria-checked="draft.color === '' ? 'true' : 'false'"
        :aria-label="$t('forum.colorNone')"
        :class="['flex h-6 w-6 items-center justify-center rounded-full border border-mnema-border-strong text-mnema-tertiary transition', draft.color === '' ? 'ring-2 ring-mnema-accent' : 'hover:border-mnema-text']"
        @click="draft.color = ''"
      >
        <Check v-if="draft.color === ''" class="h-3 w-3" />
      </button>
      <button
        v-for="c in TAG_COLORS"
        :key="c"
        type="button"
        role="radio"
        :aria-checked="draft.color === c ? 'true' : 'false'"
        :aria-label="$t(`forum.color.${c}`)"
        v-tooltip="$t(`forum.color.${c}`)"
        :class="['flex h-6 w-6 items-center justify-center rounded-full text-black/70 transition', tagSwatchClass(c), draft.color === c ? 'ring-2 ring-mnema-accent ring-offset-2 ring-offset-mnema-elevated' : 'opacity-80 hover:opacity-100']"
        @click="draft.color = c"
      >
        <Check v-if="draft.color === c" class="h-3 w-3" />
      </button>
    </div>

    <SwitchRow
      v-model="draft.moderated"
      data-testid="forum-tag-moderated"
      :title="$t('forum.moderatedTitle')"
      :hint="$t('forum.moderatedHint')"
    />

    <ForumTagChip :tag="preview" />
    <UnsavedChangesBar
      inline
      :dirty="form.dirty.value"
      :can-save="form.canSave.value"
      :saving="form.saving.value"
      :error="form.error.value"
      :save-label="tag ? '' : $t('forum.addTag')"
      save-testid="forum-tag-save"
      @save="form.save"
      @reset="form.reset"
    >
      <template #extra>
        <button type="button" class="btn btn-secondary h-9 px-3 text-xs" @click="cancel">{{ $t('common.cancel') }}</button>
      </template>
    </UnsavedChangesBar>
  </form>
</template>
