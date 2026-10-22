import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import RecoveryPage from './RecoveryPage.vue'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const TOKEN = 'B'.repeat(43)

async function failWith(status: number, message: string) {
  const { ApiError } = await vi.importActual<typeof import('../lib/api')>('../lib/api')
  return new ApiError(status, 'INVALID_INPUT', message)
}

beforeEach(() => {
  setLocale('en')
  apiMock.mockReset()
})

describe('RecoveryPage', () => {
  it('confirms an e-mail address with the token of the link and cleans the address bar', async () => {
    window.history.replaceState(null, '', `/verify-email#token=${TOKEN}`)
    apiMock.mockResolvedValue(undefined)
    const w = mount(RecoveryPage, { props: { link: { kind: 'verify', token: TOKEN } } })
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/auth/email/verify', { method: 'POST', json: { token: TOKEN } })
    expect(w.get('[data-testid="recovery-message"]').text()).toBe('Your e-mail address is confirmed.')
    expect(window.location.hash).toBe('')
    await w.get('button').trigger('click')
    expect(w.emitted('done')).toHaveLength(1)
  })

  it('says so when the address belongs to another account or the link is dead', async () => {
    apiMock.mockRejectedValueOnce(await failWith(409, 'x'))
    let w = mount(RecoveryPage, { props: { link: { kind: 'verify', token: TOKEN } } })
    await flushPromises()
    expect(w.get('[data-testid="recovery-message"]').text()).toContain('Another account')

    apiMock.mockRejectedValueOnce(await failWith(400, 'x'))
    w = mount(RecoveryPage, { props: { link: { kind: 'verify', token: TOKEN } } })
    await flushPromises()
    expect(w.get('[data-testid="recovery-message"]').text()).toContain('invalid or has expired')
  })

  it('shows a dead link at once when the link carries no usable token', () => {
    const w = mount(RecoveryPage, { props: { link: { kind: 'reset', token: '' } } })
    expect(w.get('[data-testid="recovery-message"]').text()).toContain('invalid or has expired')
    expect(apiMock).not.toHaveBeenCalled()
  })

  it('sets the new password and keeps the form for a rejected one', async () => {
    const w = mount(RecoveryPage, { props: { link: { kind: 'reset', token: TOKEN } } })
    await w.get('#recovery-password').setValue('short')
    await w.get('#recovery-repeat').setValue('short')
    await w.get('form').trigger('submit')
    expect(apiMock).not.toHaveBeenCalled()
    expect(w.get('[role="alert"]').text()).toContain('10')

    await w.get('#recovery-password').setValue('a-long-password')
    await w.get('#recovery-repeat').setValue('another-long-one')
    await w.get('form').trigger('submit')
    expect(w.get('[role="alert"]').text()).toMatch(/match/i)

    apiMock.mockRejectedValueOnce(await failWith(400, 'The password is too long'))
    await w.get('#recovery-repeat').setValue('a-long-password')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.find('form').exists()).toBe(true)

    apiMock.mockResolvedValueOnce(undefined)
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/auth/password-reset/confirm', { method: 'POST', json: { token: TOKEN, new_password: 'a-long-password' } })
    expect(w.get('[data-testid="recovery-message"]').text()).toContain('password is changed')
  })

  it('ends the form when the server calls the link dead', async () => {
    apiMock.mockRejectedValueOnce(await failWith(400, 'This link is invalid or has expired.'))
    const w = mount(RecoveryPage, { props: { link: { kind: 'reset', token: TOKEN } } })
    await w.get('#recovery-password').setValue('a-long-password')
    await w.get('#recovery-repeat').setValue('a-long-password')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.find('form').exists()).toBe(false)
    expect(w.get('[data-testid="recovery-message"]').text()).toContain('invalid or has expired')
  })
})
