// The state behind the "Invite people" dialog: a ready invite link for one
// channel, the expiry choices the server's limits allow, and the member's own
// invites with a way to revoke them. The server enforces every limit again.
import { computed, ref } from 'vue'
import { api, caughtErrorMessage } from '../../shared/lib/api'
import { decodeAuthInvite, decodeAuthMemberInviteList } from '../../types/rest'
import type { AuthInvite, AuthMemberInviteLimits } from '../../types/rest'
import { inviteLink } from '../../shared/lib/inviteLink'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { t } from '../../i18n'

const OWN_INVITES = '/api/me/invites'
/** The validity a member can pick, in hours, before the server's longest one cuts the list. */
const CHOICES = [1, 24, 168]
const DEFAULT_HOURS = 24

/** The validity choices within the limit: the usual ones that fit, or the limit itself when none does. */
export function expiryChoices(maxHours: number): number[] {
  const fitting = CHOICES.filter(h => h <= maxHours)
  return fitting.length ? fitting : [maxHours]
}

export function useChannelInvite(channelId: () => string) {
  const limits = ref<AuthMemberInviteLimits | null>(null)
  const own = ref<AuthInvite[]>([])
  const invite = ref<AuthInvite | null>(null)
  const hours = ref(DEFAULT_HOURS)
  const busy = ref(true)
  const error = ref('')
  const origin = useServerFeaturesStore()

  const link = computed(() => (invite.value ? inviteLink(origin.shareOrigin, invite.value.code) : ''))
  const choices = computed(() => expiryChoices(limits.value?.max_expires_in_hours ?? DEFAULT_HOURS))

  async function loadOwn() {
    const list = await api(OWN_INVITES, { decode: decodeAuthMemberInviteList })
    limits.value = list.limits
    own.value = list.invites
  }

  /** Makes the link for `h` hours; the one this dialog made before is revoked, so choosing again does not pile invites up. */
  async function make(h: number) {
    busy.value = true
    error.value = ''
    const previous = invite.value
    try {
      const made = await api(OWN_INVITES, { method: 'POST', json: { channel_id: channelId(), expires_in_hours: h }, decode: decodeAuthInvite })
      hours.value = h
      invite.value = made
      own.value = [made, ...own.value.filter(i => i.id !== previous?.id)]
      if (previous) await api(`${OWN_INVITES}/${previous.id}`, { method: 'DELETE' }).catch(() => {})
    } catch (e) {
      error.value = caughtErrorMessage(e, t('memberInvites.failed'))
    } finally {
      busy.value = false
    }
  }

  async function start() {
    try {
      await loadOwn()
    } catch (e) {
      error.value = caughtErrorMessage(e, t('memberInvites.failed'))
      busy.value = false
      return
    }
    await make(Math.min(DEFAULT_HOURS, choices.value[choices.value.length - 1] ?? DEFAULT_HOURS))
  }

  /** Revokes one of the member's invites; false when the server refused. */
  async function revoke(target: AuthInvite): Promise<boolean> {
    try {
      await api(`${OWN_INVITES}/${target.id}`, { method: 'DELETE' })
    } catch {
      return false
    }
    own.value = own.value.filter(i => i.id !== target.id)
    if (invite.value?.id === target.id) invite.value = null
    return true
  }

  return { limits, own, invite, hours, busy, error, link, choices, start, make, revoke }
}
