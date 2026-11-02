// The roster actions of the events store: roles, sign-ups and the notice that a
// sign-up changed without the member's doing. Every answer is the event as it
// stands, so the store's list follows it.
import * as roles from '../lib/eventRolesApi'
import type { RoleInput } from '../lib/eventRolesApi'
import type { CommunityeventsCommunityEvent as CommunityEvent } from '../types/rest'
import type { ServerEventPayloads } from '../types/events'
import { useToastStore } from './toast'
import { t } from '../i18n'

type Act = <T extends CommunityEvent>(fn: () => Promise<T>) => Promise<T>

export function eventRosterActions(act: Act, drop: (id: string) => void) {
  return {
    /** One event with its sign-ups. */
    fetchDetail: (id: string) => act(() => roles.fetchEvent(id)),
    setRoles: (id: string, list: RoleInput[]) => act(() => roles.setEventRoles(id, list)),
    deleteRole: (id: string, roleId: string, moveTo?: string) => act(() => roles.deleteEventRole(id, roleId, moveTo)),
    signUp: (id: string, roleId: string) => act(() => roles.signUpForRole(id, roleId)),
    // The answer is null when the member can no longer view the event: it leaves the list like a retracted one.
    leaveRole: async (id: string): Promise<CommunityEvent | null> => {
      const left = await roles.leaveRole(id)
      if (!left) {
        drop(id)
        return null
      }
      return act(() => Promise.resolve(left))
    },
    removeSignup: (id: string, userId: string) => act(() => roles.removeSignup(id, userId)),
    // The member moved up from a waitlist or was removed: tell them calmly and read the event again.
    handleSignupNotice: (p: ServerEventPayloads['event_signup_notice']) => {
      const key = { promoted: 'events.rolePromoted', removed: 'events.roleRemoved', waitlisted: 'events.roleWaitlisted' }[p.kind]
      useToastStore().info(t(key, { role: p.role_name, title: p.title }))
      void act(() => roles.fetchEvent(p.event_id)).catch(err => console.warn('Failed to refresh the event', err))
    }
  }
}
