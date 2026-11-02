// The roles, sign-ups and role templates of community events.
import { api } from './api'
import { arrayDecoder } from '../types/validation'
import {
  decodeCommunityeventsCommunityEvent,
  decodeCommunityeventsRoleTemplate,
  isCommunityeventsRoleTemplate,
  type CommunityeventsCommunityEvent as CommunityEvent,
  type CommunityeventsRoleInput as RoleInput,
  type CommunityeventsRoleTemplate as RoleTemplate
} from '../types/rest'

export type { RoleInput, RoleTemplate }

const decodeTemplates = arrayDecoder('role templates', isCommunityeventsRoleTemplate)

const eventPath = (id: string) => `/api/events/${encodeURIComponent(id)}`

/** One event with its sign-ups (the list and the broadcasts leave them out). */
export function fetchEvent(id: string): Promise<CommunityEvent> {
  return api(eventPath(id), { decode: decodeCommunityeventsCommunityEvent })
}

/** Replaces the roles of an event, in this order; a role with an id keeps its sign-ups. */
export function setEventRoles(id: string, roles: RoleInput[]): Promise<CommunityEvent> {
  return api(`${eventPath(id)}/roles`, { method: 'PUT', json: { roles }, decode: decodeCommunityeventsCommunityEvent })
}

/** Removes a role; moveTo takes its sign-ups over (needed when it has any). */
export function deleteEventRole(id: string, roleId: string, moveTo?: string): Promise<CommunityEvent> {
  const query = moveTo ? `?move_to=${encodeURIComponent(moveTo)}` : ''
  return api(`${eventPath(id)}/roles/${encodeURIComponent(roleId)}${query}`, { method: 'DELETE', decode: decodeCommunityeventsCommunityEvent })
}

export function signUpForRole(id: string, roleId: string): Promise<CommunityEvent> {
  return api(`${eventPath(id)}/signup`, { method: 'PUT', json: { role_id: roleId }, decode: decodeCommunityeventsCommunityEvent })
}

export function leaveRole(id: string): Promise<CommunityEvent> {
  return api(`${eventPath(id)}/signup`, { method: 'DELETE', decode: decodeCommunityeventsCommunityEvent })
}

/** A moderator or the creator removes another member from the roster. */
export function removeSignup(id: string, userId: string): Promise<CommunityEvent> {
  return api(`${eventPath(id)}/signup/${encodeURIComponent(userId)}`, { method: 'DELETE', decode: decodeCommunityeventsCommunityEvent })
}

export function fetchRoleTemplates(): Promise<RoleTemplate[]> {
  return api('/api/event-role-templates', { decode: decodeTemplates })
}

export function createRoleTemplate(name: string, roles: RoleInput[], enabled = true): Promise<RoleTemplate> {
  return api('/api/event-role-templates', { method: 'POST', json: { name, roles, enabled }, decode: decodeCommunityeventsRoleTemplate })
}

export function setRoleTemplateEnabled(id: string, enabled: boolean): Promise<RoleTemplate> {
  return api(`/api/event-role-templates/${encodeURIComponent(id)}`, { method: 'PATCH', json: { enabled }, decode: decodeCommunityeventsRoleTemplate })
}

export async function deleteRoleTemplate(id: string): Promise<void> {
  await api(`/api/event-role-templates/${encodeURIComponent(id)}`, { method: 'DELETE' })
}
