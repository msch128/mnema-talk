<script setup lang="ts">
import type { PropType } from 'vue'
import type { User as ProfileUser } from '../types/domain'
// What can be done with another member, in quiet zones instead of one pill
// row: their roles (with a way to change them for those who may), moderation
// (timeout, ban) for moderators, and blocking for everyone.
import { computed, ref } from 'vue'
import { Ban, Lock, Pencil, Timer, TimerOff } from '@lucide/vue'
import { roleColorCss } from '../lib/permissions'
import { moderationOffer, openTimeout, openBan, liftTimeout } from '../lib/moderationActions'
import { useBlocksStore } from '../stores/blocks'
import { usePermissionsStore } from '../stores/permissions'
import { useDmActions } from '../composables/useDmActions'
import MemberRolesDialog from './MemberRolesDialog.vue'

const props = defineProps({
  profileUser: { type: Object as PropType<Partial<ProfileUser>>, required: true },
  isSelf: { type: Boolean, default: false },
  isBot: { type: Boolean, default: false }
})

const permissions = usePermissionsStore()
const blocks = useBlocksStore()
const dmActions = useDmActions()

const profileId = computed(() => props.profileUser.id ?? '')
const roles = computed(() => permissions.rolesOf(profileId.value))
const subject = computed(() => ({ id: profileId.value, username: props.profileUser.username, display_name: props.profileUser.display_name }))
const rolesDialogUser = computed(() => ({ id: profileId.value, username: props.profileUser.username ?? '', display_name: props.profileUser.display_name ?? '', role: props.profileUser.role ?? 'user' }))
const showRolesDialog = ref(false)
// Roles are given by someone with Manage roles and a higher rank; never the Owner's, never one's own.
const canGiveRoles = computed(() => !!profileId.value && !props.isSelf && !props.isBot && props.profileUser.role !== 'admin' && permissions.can('manage_roles') && permissions.outranksMember(profileId.value))
const offer = computed(() => moderationOffer({ id: profileId.value, role: props.profileUser.role, display_name: props.profileUser.display_name, username: props.profileUser.username }))
const showModeration = computed(() => !props.isBot && (offer.value.timeout || offer.value.ban))
const showBlock = computed(() => dmActions.canBlock(profileId.value) && !props.isBot)

const hasAny = computed(() => roles.value.length > 0 || canGiveRoles.value || showModeration.value || showBlock.value)
const label = 'font-mono text-[11px] uppercase tracking-wider text-mnema-tertiary'
const action = 'btn btn-ghost btn-sm max-md:min-h-11 hover:text-mnema-danger'
</script>

<template>
  <div v-if="hasAny" class="my-3 divide-y divide-mnema-hairline border-b border-mnema-hairline pb-3">
    <section v-if="roles.length || canGiveRoles" class="py-3 first:pt-0 last:pb-0" data-testid="profile-roles" aria-labelledby="profile-roles-label">
      <h3 id="profile-roles-label" :class="[label, 'mb-1.5']">{{ $t('profile.rolesTitle') }}</h3>
      <div class="flex flex-wrap items-center gap-1.5">
        <span
          v-for="role in roles"
          :key="role.id"
          class="inline-flex items-center gap-1.5 rounded-md border border-mnema-border px-2 py-0.5 text-xs text-mnema-text"
        >
          <span class="h-2 w-2 rounded-full border border-mnema-border-strong" :style="{ background: roleColorCss(role.color) || 'transparent' }"></span>{{ role.name }}
          <Lock v-if="role.managed" class="h-3 w-3 text-mnema-tertiary" v-tooltip.visual="$t(role.bot_user_id ? 'roles.botRoleHint' : 'roles.managedHint')" role="img" :aria-label="$t(role.bot_user_id ? 'roles.botRoleHint' : 'roles.managedHint')" />
        </span>
        <button
          v-if="canGiveRoles"
          type="button"
          data-testid="profile-edit-roles"
          class="btn btn-secondary btn-sm border-dashed max-md:min-h-11"
          @click="showRolesDialog = true"
        ><Pencil class="h-3 w-3" aria-hidden="true" />{{ $t('memberRoles.edit') }}</button>
      </div>
    </section>

    <section v-if="showModeration" class="py-3 first:pt-0 last:pb-0" data-testid="profile-moderation" aria-labelledby="profile-moderation-label">
      <h3 id="profile-moderation-label" :class="[label, 'mb-1.5']">{{ $t('profile.moderationTitle') }}</h3>
      <div class="-ml-2 flex flex-wrap items-center gap-1">
        <button v-if="offer.timeout" type="button" data-testid="profile-timeout" :class="action" @click="openTimeout(subject)"><Timer class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('moderation.timeoutAction') }}</button>
        <button v-if="offer.removeTimeout" type="button" data-testid="profile-untimeout" :class="action" @click="liftTimeout(subject)"><TimerOff class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('moderation.removeTimeout') }}</button>
        <button v-if="offer.ban" type="button" data-testid="profile-ban" :class="action" @click="openBan(subject)"><Ban class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('moderation.banAction') }}</button>
      </div>
    </section>

    <!-- Blocking is private: they are not told, their messages fold away -->
    <section v-if="showBlock" class="py-3 first:pt-0 last:pb-0" data-testid="profile-block-row">
      <button type="button" data-testid="profile-block" class="btn btn-ghost btn-sm -ml-2 max-md:min-h-11 hover:text-mnema-danger" @click="dmActions.toggleBlock(subject)">{{ $t(blocks.isBlocked(profileId) ? 'blocks.unblock' : 'blocks.block') }}</button>
      <p class="mt-0.5 text-xs text-mnema-tertiary">{{ $t(blocks.isBlocked(profileId) ? 'blocks.blockedHint' : 'blocks.hint') }}</p>
    </section>

  </div>
  <MemberRolesDialog v-if="showRolesDialog" :user="rolesDialogUser" @close="showRolesDialog = false" />
</template>
