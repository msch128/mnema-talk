import { expect, it, vi } from 'vitest'
import { registerThenLogin, type CapturedPort, type Reply } from './nativeRegistration'
const input = { username: 'synthetic-user', display_name: 'Synthetic User', password: 'synthetic-test-password', invite_code: 'synthetic-invite' }
function fixture() {
  let current = true
  const port: CapturedPort = { isCurrent: () => current,
    call: vi.fn(async command => ({ status: command === 'native_auth_register' ? 201 : 200, body: 'created-account' })),
    retireCaptured: vi.fn(async () => {}), userId: body => { if (typeof body !== 'string') throw new Error('Invalid user'); return body } }
  return { port, retire: () => { current = false } }
}
it('requires a second acknowledged matching login, preserving exact credential bytes', async () => {
  const h = fixture()
  expect(await registerThenLogin(h.port, input)).toEqual({ kind: 'authenticated', reply: { status: 200, body: 'created-account' } })
  expect(h.port.call).toHaveBeenNthCalledWith(1, 'native_auth_register', input)
  expect(h.port.call).toHaveBeenNthCalledWith(2, 'native_auth_login', { username: input.username, password: input.password })
  expect(h.port.retireCaptured).not.toHaveBeenCalled()
})
it('does not create an account when the captured profile has already retired', async () => {
  const h = fixture(); h.retire()
  expect(await registerThenLogin(h.port, input)).toEqual({ kind: 'login_required', reason: 'scope_retired' })
  expect(h.port.call).not.toHaveBeenCalled()
})
it.each(['unknown', 'rejected', 'invalid'] as const)('never logs in after %s creation acknowledgement', async mode => {
  const h = fixture()
  vi.mocked(h.port.call).mockImplementationOnce(async () => {
    if (mode === 'unknown') throw new Error('Lost ACK')
    return mode === 'invalid' ? { status: 201, body: null } : { status: 409, body: 'invite-used' }
  })
  expect(await registerThenLogin(h.port, input)).toEqual(mode === 'rejected' ? { kind: 'rejected', reply: { status: 409, body: 'invite-used' } } : { kind: 'login_required', reason: mode === 'unknown' ? 'ack_unknown' : 'invalid_ack' })
  expect(h.port.call).toHaveBeenCalledOnce()
})
it.each(['lost', 'unauthorized', 'mismatch', 'invalid'] as const)('requires manual login after %s login result and seals only a mismatched accepted identity', async mode => {
  const h = fixture()
  vi.mocked(h.port.call).mockImplementation(async command => {
    if (command === 'native_auth_register') return { status: 201, body: 'created-account' }
    if (mode === 'lost') throw new Error('Lost login ACK')
    return { status: mode === 'unauthorized' ? 401 : 200, body: mode === 'invalid' ? null : 'other-account' }
  })
  expect(await registerThenLogin(h.port, input)).toEqual({ kind: 'login_required', reason: ['mismatch', 'invalid'].includes(mode) ? 'identity_mismatch' : 'login_failed' })
  expect(h.port.retireCaptured).toHaveBeenCalledTimes(['mismatch', 'invalid'].includes(mode) ? 1 : 0)
})
it.each(['native_auth_register', 'native_auth_login'] as const)('rejects a late %s result after scope retirement', async command => {
  const h = fixture(); let resolve!: (reply: Reply) => void
  vi.mocked(h.port.call).mockImplementation(async stage => stage === command ? new Promise<Reply>(r => { resolve = r }) : { status: 201, body: 'created-account' })
  const pending = registerThenLogin(h.port, input)
  await Promise.resolve(); h.retire(); resolve({ status: command === 'native_auth_register' ? 201 : 200, body: 'created-account' })
  expect(await pending).toEqual({ kind: 'login_required', reason: 'scope_retired' })
  expect(h.port.call).toHaveBeenCalledTimes(command === 'native_auth_register' ? 1 : 2)
  expect(h.port.retireCaptured).not.toHaveBeenCalled()
})
it('surfaces failed captured cleanup without treating an unrelated identity as authenticated', async () => {
  const h = fixture()
  vi.mocked(h.port.call).mockImplementation(async command => ({ status: command === 'native_auth_register' ? 201 : 200, body: command }))
  vi.mocked(h.port.retireCaptured).mockRejectedValue(new Error('Cleanup unavailable'))
  await expect(registerThenLogin(h.port, input)).rejects.toThrow('Cleanup unavailable')
})
it.each([1, 2])('rechecks the captured scope after identity decoding #%s', async boundary => {
  const h = fixture(); let decoded = 0
  h.port.userId = body => { if (++decoded === boundary) h.retire(); return String(body) }
  expect(await registerThenLogin(h.port, input)).toEqual({ kind: 'login_required', reason: 'scope_retired' })
  expect(h.port.call).toHaveBeenCalledTimes(boundary)
})
