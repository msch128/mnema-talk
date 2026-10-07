import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import AccountMenu from './AccountMenu.vue'
import UserBar from './UserBar.vue'
import { userFixture, fixtureId } from '../test-fixtures.fixture'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { useAppVersionStore } from '../stores/appVersion'
import { systemStatus } from './systemStatus.fixture'
import { setLocale, t } from '../i18n'
const h = vi.hoisted(() => ({ stop: vi.fn() }))
vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => ({ stopScreenShare: h.stop }) }))
vi.mock('../lib/api', async original => ({ ...(await original<typeof import('../lib/api')>()), api: vi.fn() }))
let w: VueWrapper
let trigger: HTMLButtonElement
beforeEach(() => { setActivePinia(createPinia()); setLocale('en'); h.stop.mockClear(); trigger = document.createElement('button'); document.body.append(trigger); useAuthStore().user = userFixture({ role: 'admin', display_name: 'Ada' }) })
afterEach(() => { w?.unmount(); document.body.innerHTML = ''; vi.restoreAllMocks() })
function mountMenu() { w = mount(AccountMenu, { props: { trigger }, attachTo: document.body }); return w }
function item(text: string) { const found = w.findAll('button').find(b => b.text().includes(text)); if (!found) throw new Error(`Missing action ${text}`); return found }
async function key(key: string) { await w.trigger('keydown', { key }); await nextTick() }
it('opens profile, audio, connection details, screen share, admin, legal and logout actions', async () => {
  const auth = useAuthStore(); const chat = useChatStore(); const voice = useVoiceStore()
  const profile = vi.spyOn(chat, 'openUserProfile').mockResolvedValue(); const logout = vi.spyOn(auth, 'logout').mockResolvedValue()
  voice.setChannel(fixtureId(2))
  mountMenu(); await flushPromises()
  await item(t('account.editProfile')).trigger('click'); expect(profile).toHaveBeenCalledWith(auth.user)
  await item(t('account.audio')).trigger('click'); expect(voice.showAudioSettings).toBe(true)
  await item(t('voice.panel.details')).trigger('click'); expect(voice.showStatsModal).toBe(true)
  await item(t('voice.share')).trigger('click'); expect(voice.showScreenShareModal).toBe(true)
  voice.isScreenSharing = true; await nextTick(); await item(t('voice.stopShare')).trigger('click'); expect(h.stop).toHaveBeenCalledTimes(1)
  await item(t('menu.adminConsole')).trigger('click'); expect(w.emitted('open-admin')).toHaveLength(1)
  await item(t('menu.legal')).trigger('click'); expect(w.emitted('open-legal')).toHaveLength(1)
  await item(t('account.signOut')).trigger('click'); expect(logout).toHaveBeenCalledTimes(1)
  expect(document.activeElement).toBe(trigger)
  auth.user = null; await nextTick(); await item(t('account.editProfile')).trigger('click'); expect(profile).toHaveBeenCalledTimes(1)
})
it('handles same language, both language save outcomes and submenu toggle', async () => {
  const auth = useAuthStore(); const change = vi.spyOn(auth, 'changeLocale').mockResolvedValue()
  mountMenu(); await item('Language').trigger('click'); await w.find('button[lang="en"]').trigger('click'); expect(change).not.toHaveBeenCalled()
  await item('Language').trigger('click'); await item('Language').trigger('click'); expect(w.find('[data-submenu]').exists()).toBe(false)
  await item('Language').trigger('click'); await w.find('button[lang="de"]').trigger('click'); await flushPromises(); expect(change).toHaveBeenCalledWith('de'); expect(useToastStore().toasts.at(-1)?.type).toBe('success')
  change.mockRejectedValueOnce(new Error('offline'))
  await item('Language').trigger('click'); await w.find('button[lang="de"]').trigger('click'); await flushPromises(); expect(useToastStore().toasts.at(-1)?.text).toBe('offline')
  change.mockRejectedValueOnce(new Error(''))
  await item('Language').trigger('click'); await w.find('button[lang="de"]').trigger('click'); await flushPromises(); expect(useToastStore().toasts.at(-1)?.type).toBe('error')
})
it('supports arrow navigation, submenu Escape/Left, Tab and ordinary keys', async () => {
  mountMenu(); await flushPromises()
  // happy-dom has no layout; these menu buttons are visible in the actual browser.
  const items = w.findAll<HTMLElement>('[role^="menuitem"]')
  items.forEach(i => Object.defineProperty(i.element, 'offsetParent', { configurable: true, get: () => w.element }))
  expect(document.activeElement).toBe(items[0]!.element)
  await key('ArrowDown'); expect(document.activeElement).toBe(items[1]!.element)
  await key('ArrowUp'); expect(document.activeElement).toBe(items[0]!.element)
  const lang = w.find<HTMLElement>('[data-lang-trigger]'); lang.element.focus(); await key('ArrowRight'); await flushPromises(); expect(w.find('[data-submenu]').exists()).toBe(true)
  await key('ArrowLeft'); expect(w.find('[data-submenu]').exists()).toBe(false); expect(document.activeElement).toBe(lang.element)
  await key('ArrowRight'); await key('Escape'); expect(w.find('[data-submenu]').exists()).toBe(false)
  const count = w.emitted('close')?.length ?? 0
  await key('Enter'); expect(w.emitted('close')?.length ?? 0).toBe(count)
  await key('ArrowRight'); expect(w.find('[data-submenu]').exists()).toBe(true)
  await key('Escape'); await key('Escape'); expect(w.emitted('close')).toHaveLength(count + 1)
  lang.element.focus(); await key('Tab'); expect(document.activeElement).toBe(lang.element)
})
it('ignores internal and trigger pointers, closes outside, removes document listener', async () => {
  mountMenu(); await flushPromises()
  w.element.dispatchEvent(new Event('pointerdown', { bubbles: true })); trigger.dispatchEvent(new Event('pointerdown', { bubbles: true })); expect(w.emitted('close')).toBeUndefined()
  document.body.dispatchEvent(new Event('pointerdown', { bubbles: true })); expect(w.emitted('close')).toHaveLength(1)
  const onClose = vi.fn(); await w.setProps({ onClose }); w.unmount(); document.body.dispatchEvent(new Event('pointerdown', { bubbles: true })); expect(onClose).not.toHaveBeenCalled()
})
it('UserBar handles missing account, chosen presence, menu exclusivity and forwards menu events', async () => {
  const auth = useAuthStore(); const chat = useChatStore(); const profile = vi.spyOn(chat, 'openUserProfile').mockResolvedValue()
  auth.user = null; w = mount(UserBar, { attachTo: document.body }); await w.find('[data-testid="own-profile-button"]').trigger('click'); expect(profile).not.toHaveBeenCalled()
  auth.user = userFixture({ presence: 'focus' }); await nextTick(); expect(w.find('[data-testid="own-subline"]').text()).toBe('Focused')
  await w.find('[data-testid="presence-button"]').trigger('click'); expect(w.find('[data-testid="presence-menu"]').exists()).toBe(true)
  await w.find('[data-testid="account-menu-button"]').trigger('click'); expect(w.find('[data-testid="presence-menu"]').exists()).toBe(false)
  auth.user = userFixture({ role: 'admin' }); await nextTick()
  await item(t('menu.adminConsole')).trigger('click'); expect(w.emitted('open-admin')).toHaveLength(1)
  await w.find('[data-testid="account-menu-button"]').trigger('click'); await item(t('menu.legal')).trigger('click'); expect(w.emitted('open-legal')).toHaveLength(1)
  const version = useAppVersionStore(); version.adminUpdate = { ...systemStatus().update, update_available: true, latest_version: '0.5.0' }
  await nextTick(); expect(w.find('[data-testid="admin-update-dot"]').exists()).toBe(true)
})
