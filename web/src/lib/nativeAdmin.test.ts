import { expect, it } from 'vitest'
import { mapAdminOperation } from './nativeAdmin'
const id = '10000000-0000-4000-8000-000000000001'
const other = '10000000-0000-4000-8000-000000000002'
it.each(['invites', 'users', 'system', 'system/update'])('maps the closed %s read without accepting a body', resource => {
  const path = `/api/admin/${resource}`
  expect(mapAdminOperation(path, { method: 'GET' })).toEqual({ operation: resource.replace('/', '_') })
  expect(mapAdminOperation(path, { method: 'GET', json: {} })).toBeNull()
  expect(mapAdminOperation(path, { method: 'GET', form: {} })).toBeNull()
})
it.each([['invites', 'delete_invite'], ['categories', 'delete_category'], ['channels', 'delete_channel']])('maps only an exact %s deletion', (resource, operation) => {
  const path = `/api/admin/${resource}/${id}`
  expect(mapAdminOperation(path, { method: 'DELETE' })).toEqual({ operation, id })
  expect(mapAdminOperation(path, { method: 'DELETE', json: {} })).toBeNull()
  expect(mapAdminOperation(`${path}/disable`, { method: 'DELETE' })).toBeNull()
})
it.each([['disable', 'disable_user'], ['enable', 'enable_user'], ['sessions/revoke', 'revoke_user_sessions'], ['kick', 'kick_user']])('maps %s only as an explicit user action', (suffix, operation) => {
  expect(mapAdminOperation(`/api/admin/users/${id}/${suffix}`, { method: 'POST' })).toEqual({ operation, id })
  expect(mapAdminOperation(`/api/admin/users/${id}/${suffix}`, { method: 'POST', json: {} })).toBeNull()
})
it('maps duplication, status, rename and closed partial channel updates', () => {
  expect(mapAdminOperation(`/api/admin/channels/${id}/duplicate`, { method: 'POST' })).toEqual({ operation: 'duplicate_channel', id })
  expect(mapAdminOperation(`/api/admin/users/${id}/status`, { method: 'PUT', json: { status_text: '🙂'.repeat(32) } })).toEqual({ operation: 'user_status', id, status_text: '🙂'.repeat(32) })
  expect(mapAdminOperation(`/api/admin/categories/${id}`, { method: 'PATCH', json: { name: 'Category' } })).toEqual({ operation: 'rename_category', id, name: 'Category' })
  expect(mapAdminOperation(`/api/admin/channels/${id}`, { method: 'PATCH', json: {} })).toEqual({ operation: 'update_channel', id, name: null, topic: null, user_limit: null })
  expect(mapAdminOperation(`/api/admin/channels/${id}`, { method: 'PATCH', json: { name: 'Text', topic: '', user_limit: 0 } })).toEqual({ operation: 'update_channel', id, name: 'Text', topic: '', user_limit: 0 })
})
it('preserves only valid creation defaults and explicit limits', () => {
  expect(mapAdminOperation('/api/admin/invites', { method: 'POST', json: {} })).toEqual({ operation: 'create_invite', code: '', max_uses: null, expires_in_hours: null })
  expect(mapAdminOperation('/api/admin/invites', { method: 'POST', json: { code: 'synthetic-code', max_uses: 1000, expires_in_hours: 8760 } })).toEqual({ operation: 'create_invite', code: 'synthetic-code', max_uses: 1000, expires_in_hours: 8760 })
  expect(mapAdminOperation('/api/admin/categories', { method: 'POST', json: { name: 'Category' } })).toEqual({ operation: 'create_category', name: 'Category', sort_order: 0 })
  expect(mapAdminOperation('/api/admin/categories', { method: 'POST', json: { name: 'Category', sort_order: -2147483648 } })).toMatchObject({ sort_order: -2147483648 })
  expect(mapAdminOperation('/api/admin/channels', { method: 'POST', json: { name: 'Channel' } })).toEqual({ operation: 'create_channel', category_id: null, name: 'Channel', kind: 'text', topic: '', sort_order: 0 })
  expect(mapAdminOperation('/api/admin/channels', { method: 'POST', json: { name: 'Voice', category_id: id, type: 'voice', topic: 'Topic', sort_order: 2147483647 } })).toMatchObject({ category_id: id, kind: 'voice', topic: 'Topic', sort_order: 2147483647 })
})
it.each([
  ['users', 'PUT', { status_text: 'x'.repeat(33) }, '/status'], ['users', 'PUT', { status_text: 'x\0y' }, '/status'],
  ['categories', 'PATCH', { name: '' }, ''], ['categories', 'PATCH', { name: 42 }, ''],
  ['channels', 'PATCH', { name: 'x'.repeat(65) }, ''], ['channels', 'PATCH', { topic: 'x'.repeat(256) }, ''],
  ['channels', 'PATCH', { user_limit: -1 }, ''], ['channels', 'PATCH', { user_limit: 1000 }, ''], ['channels', 'PATCH', { user_limit: 0.5 }, ''],
  ['channels', 'PATCH', { name: 'Valid', user_id: id }, '']
])('rejects malformed target operations for %s', (resource, method, json, suffix) => {
  expect(mapAdminOperation(`/api/admin/${resource}/${id}${suffix}`, { method: String(method), json })).toBeNull()
})
it.each([
  ['invites', { code: 'short' }], ['invites', { code: true }], ['invites', { max_uses: 0 }], ['invites', { max_uses: 1001 }],
  ['invites', { expires_in_hours: 0 }], ['invites', { expires_in_hours: 8761 }], ['invites', { code: 'valid-code', extra: true }],
  ['categories', { name: 'Valid', sort_order: 2147483648 }], ['categories', { name: 'Valid', sort_order: -2147483649 }],
  ['categories', {}], ['channels', { name: 'Valid', category_id: 'forged' }], ['channels', { name: 'Valid', type: 'dm' }],
  ['channels', { name: 'Valid', topic: false }], ['channels', { name: 'Valid', sort_order: 0.5 }]
])('rejects malformed %s creation before IPC', (resource, json) => {
  expect(mapAdminOperation(`/api/admin/${resource}`, { method: 'POST', json })).toBeNull()
})
it('validates an entire layout atomically, including duplicate and cross-type identifiers', () => {
  const categories = [{ id, sort_order: 0 }], channels = [{ id: other, category_id: id, sort_order: 1 }]
  expect(mapAdminOperation('/api/admin/layout', { method: 'PUT', json: { categories, channels } })).toEqual({ operation: 'layout', categories, channels })
  expect(mapAdminOperation('/api/admin/layout', { method: 'PUT', json: { categories: [], channels: [{ id, sort_order: 0 }] } })).toMatchObject({ channels: [{ id, category_id: null, sort_order: 0 }] })
  for (const json of [null, [], {}, { categories, channels, grant: true }, { categories: Array(501).fill(categories[0]), channels: [] },
    { categories: [null], channels }, { categories: [{ id: 'forged', sort_order: 0 }], channels },
    { categories: [{ id, sort_order: false }], channels }, { categories: [...categories, ...categories], channels },
    { categories, channels: [null] }, { categories, channels: [{ id: other }] },
    { categories, channels: [...channels, ...channels] }, { categories, channels: [{ ...channels[0], category_id: 'forged' }] }]) {
    expect(mapAdminOperation('/api/admin/layout', { method: 'PUT', json })).toBeNull()
  }
})
it('denies unknown endpoints and noncanonical target identifiers', () => {
  for (const path of ['/api/admin/users/__proto__', '/api/admin/users/00000000-0000-0000-0000-000000000000/kick', '/api/admin/system/install', `/api/admin/users/${id}/duplicate`, `/api/admin/channels/${id}/enable`]) {
    expect(mapAdminOperation(path, { method: 'POST' })).toBeNull()
  }
})
