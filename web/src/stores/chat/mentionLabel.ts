import type { Member } from '../../types/domain'

/**
 * The display name that stands for a mentioned username in previews, or
 * undefined where the username is already the name to show (no display name,
 * or the same word) or no member has it (a deleted account).
 */
export function memberLabel(members: readonly Pick<Member, 'username' | 'display_name'>[], lowerUsername: string): string | undefined {
  const member = members.find(m => (m.username || '').toLowerCase() === lowerUsername)
  const display = member?.display_name
  return display && display.toLowerCase() !== lowerUsername ? display : undefined
}
