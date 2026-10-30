import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ShortcutsDialog from './ShortcutsDialog.vue'
import { i18nPlugin, setLocale, t } from '../i18n'
import { SHORTCUT_GROUPS } from '../lib/shortcuts'
import { useVoiceStore } from '../stores/voice'

const mountDialog = () => mount(ShortcutsDialog, { attachTo: document.body, global: { plugins: [i18nPlugin] } })

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
  setLocale('en')
  document.body.replaceChildren()
})

describe('ShortcutsDialog', () => {
  it('lists every group with a heading and a sentence for every shortcut, in both languages', () => {
    for (const locale of ['en', 'de'] as const) {
      setLocale(locale)
      for (const group of SHORTCUT_GROUPS) {
        expect(t(`shortcuts.group.${group.id}`), `${locale} ${group.id}`).not.toBe(`shortcuts.group.${group.id}`)
        if (group.hint) expect(t(`shortcuts.groupHint.${group.id}`)).not.toBe(`shortcuts.groupHint.${group.id}`)
        for (const item of group.items) expect(t(`shortcuts.${group.id}.${item.id}`), `${locale} ${group.id}.${item.id}`).not.toBe(`shortcuts.${group.id}.${item.id}`)
      }
    }
    setLocale('en')
    const w = mountDialog()
    for (const group of SHORTCUT_GROUPS) {
      expect(w.get(`[data-testid="shortcuts-group-${group.id}"]`).findAll('li')).toHaveLength(group.items.length)
    }
    w.unmount()
  })

  it('shows the search shortcut with Ctrl and the list shortcut with both ways to open it', () => {
    const w = mountDialog()
    const search = w.get('[data-shortcut="search"]')
    expect(search.findAll('kbd').map(k => k.text())).toEqual([navigator.platform.match(/mac/i) ? '⌘' : 'Ctrl', 'K'])
    const list = w.get('[data-shortcut="shortcuts"]')
    expect(list.text()).toContain('or')
    expect(list.findAll('kbd').map(k => k.text())).toContain('?')
    w.unmount()
  })

  it('shows the member’s own push-to-talk key, or says it is not set', async () => {
    const voice = useVoiceStore()
    voice.pttKey = ''
    const w = mountDialog()
    expect(w.get('[data-shortcut="ptt"]').text()).toContain('not set')
    voice.pttKey = 'F8'
    await w.vm.$nextTick()
    expect(w.get('[data-shortcut="ptt"]').findAll('kbd').map(k => k.text())).toEqual(['F8'])
    w.unmount()
  })

  it('closes with Escape', async () => {
    const w = mountDialog()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await w.vm.$nextTick()
    expect(w.emitted('close')).toBeTruthy()
    w.unmount()
  })
})
