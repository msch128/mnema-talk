// The admin console's tabs and how they are grouped in its navigation.

export type AdminTab = 'users' | 'roles' | 'channels' | 'invites' | 'onboarding' | 'access' | 'emoji' | 'reports' | 'automod' | 'raid' | 'bans' | 'bots' | 'import' | 'media' | 'audit' | 'system' | 'status'

export const NAV_GROUPS: readonly { id: string; tabs: readonly AdminTab[] }[] = [
  { id: 'community', tabs: ['users', 'roles', 'channels', 'invites', 'onboarding', 'access', 'emoji'] },
  { id: 'moderation', tabs: ['reports', 'automod', 'raid', 'bans', 'audit'] },
  { id: 'integrations', tabs: ['bots', 'import'] },
  { id: 'operations', tabs: ['media', 'system', 'status'] }
]

/** The tabs in navigation order. */
export const ALL_TABS: readonly AdminTab[] = NAV_GROUPS.flatMap(group => group.tabs)
