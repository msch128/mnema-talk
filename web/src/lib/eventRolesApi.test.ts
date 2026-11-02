import { describe, it, expect, beforeEach, vi } from 'vitest'
import { api } from './api'
import {
  fetchEvent, setEventRoles, deleteEventRole, signUpForRole, leaveRole, removeSignup,
  fetchRoleTemplates, createRoleTemplate, setRoleTemplateEnabled, deleteRoleTemplate
} from './eventRolesApi'

vi.mock('./api', () => ({ api: vi.fn() }))
const apiMock = vi.mocked(api) as unknown as ReturnType<typeof vi.fn>

type Opts = { method?: string; json?: unknown; decode?: (v: unknown) => unknown }
function last(): [string, Opts] {
  const call = apiMock.mock.calls.at(-1)
  if (!call) throw new Error('api was not called')
  return [call[0] as string, (call[1] ?? {}) as Opts]
}

const template = { id: 'a0000000-0000-4000-8000-000000000001', name: 'Raid', roles: [{ name: 'Tank', capacity: 2 }], enabled: true, preset: false }

beforeEach(() => apiMock.mockReset())

describe('event roles api', () => {
  it('reads one event', async () => {
    await fetchEvent('e/1')
    expect(last()[0]).toBe('/api/events/e%2F1')
  })

  it('replaces the roles in order', async () => {
    await setEventRoles('e1', [{ name: 'Tank', capacity: 1 }])
    const [path, opts] = last()
    expect(path).toBe('/api/events/e1/roles')
    expect(opts.method).toBe('PUT')
    expect(opts.json).toEqual({ roles: [{ name: 'Tank', capacity: 1 }] })
  })

  it('deletes a role with or without a role to move the sign-ups to', async () => {
    await deleteEventRole('e1', 'r1')
    expect(last()[0]).toBe('/api/events/e1/roles/r1')
    expect(last()[1].method).toBe('DELETE')
    await deleteEventRole('e1', 'r1', 'r 2')
    expect(last()[0]).toBe('/api/events/e1/roles/r1?move_to=r%202')
  })

  it('signs up, leaves and removes a member', async () => {
    await signUpForRole('e1', 'r1')
    expect(last()[1]).toMatchObject({ method: 'PUT', json: { role_id: 'r1' } })
    expect(last()[0]).toBe('/api/events/e1/signup')
    await leaveRole('e1')
    expect(last()[1].method).toBe('DELETE')
    await removeSignup('e1', 'u1')
    expect(last()[0]).toBe('/api/events/e1/signup/u1')
  })

  it('lists, creates, toggles and deletes templates', async () => {
    await fetchRoleTemplates()
    expect(last()[1].decode?.([template])).toEqual([template])
    expect(() => last()[1].decode?.([{ nope: 1 }])).toThrow()
    await createRoleTemplate('Raid', [{ name: 'Tank' }])
    expect(last()[1]).toMatchObject({ method: 'POST', json: { name: 'Raid', roles: [{ name: 'Tank' }], enabled: true } })
    await setRoleTemplateEnabled('t1', false)
    expect(last()[0]).toBe('/api/event-role-templates/t1')
    expect(last()[1]).toMatchObject({ method: 'PATCH', json: { enabled: false } })
    await deleteRoleTemplate('t1')
    expect(last()[1].method).toBe('DELETE')
  })
})
