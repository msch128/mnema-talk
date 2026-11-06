<script setup lang="ts">
import { charCount } from '../lib/textLength'
import { updateForumSettings } from '../lib/forumApi'
import type { ForumSettings } from '../lib/forumApi'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import SwitchRow from './SwitchRow.vue'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

// The forum's rules: whether a post needs a tag, and the guidelines shown above
// the new-post form. One draft with its own bar; fresh settings from outside
// replace only the fields nobody is editing.
const props = defineProps<{
  channelId: string
  settings: ForumSettings
  /** The forum has no tags to require. */
  noTags: boolean
}>()

const emit = defineEmits<{ saved: [settings: ForumSettings] }>()

const MAX_GUIDELINES = 1000
const toasts = useToastStore()

const form = useSettingsDraft<ForumSettings>({
  source: () => ({ require_tag: props.settings.require_tag, guidelines: props.settings.guidelines }),
  normalisers: { require_tag: normalise.flag, guidelines: normalise.text },
  save: async ({ draft }) => {
    const next = await updateForumSettings(props.channelId, { require_tag: draft.require_tag, guidelines: draft.guidelines.trim() })
    toasts.success(t('forum.settingsSaved'))
    emit('saved', next)
  }
})
const { draft } = form
</script>

<template>
  <section class="space-y-3" data-testid="forum-settings" @keydown="form.onKeydown">
    <h3 class="text-xs font-semibold uppercase tracking-wider text-mnema-secondary">{{ $t('forum.rulesTitle') }}</h3>
    <SwitchRow
      v-model="draft.require_tag"
      data-testid="forum-require-tag"
      :title="$t('forum.requireTagTitle')"
      :hint="noTags ? $t('forum.requireTagNoTags') : $t('forum.requireTagHint')"
    />
    <div>
      <div class="mb-1.5 flex items-center justify-between">
        <label for="forum-guidelines" class="text-sm font-semibold text-mnema-text">{{ $t('forum.guidelinesTitle') }}</label>
        <span class="text-xs tabular-nums text-mnema-tertiary">{{ charCount(draft.guidelines) }}/{{ MAX_GUIDELINES }}</span>
      </div>
      <textarea
        id="forum-guidelines"
        v-model="draft.guidelines"
        v-max-chars="MAX_GUIDELINES"
        rows="3"
        :placeholder="$t('forum.guidelinesPlaceholder')"
        class="min-h-[72px] w-full resize-y rounded-lg border border-mnema-border bg-mnema-surface p-3 text-sm text-mnema-text placeholder:text-mnema-tertiary focus:outline-hidden focus:border-mnema-accent"
      ></textarea>
    </div>
    <UnsavedChangesBar
      inline
      :dirty="form.dirty.value"
      :can-save="form.canSave.value"
      :saving="form.saving.value"
      :error="form.error.value"
      save-testid="forum-settings-save"
      @save="form.save"
      @reset="form.reset"
    />
  </section>
</template>
