import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ServerUnreachable from './ServerUnreachable.vue'
import { i18nPlugin, setLocale } from '../../i18n'

const NOW = new Date('2026-11-06T10:00:00Z').getTime()
const mountIt = (props: { trying: boolean; lastTriedAt: number | null }) => mount(ServerUnreachable, { props, global: { plugins: [i18nPlugin] } })

beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(NOW); setLocale('en') })
afterEach(() => vi.useRealTimers())

describe('ServerUnreachable', () => {
  it('asks for an immediate retry when "Try again" is pressed', async () => {
    const w = mountIt({ trying: false, lastTriedAt: NOW })
    await w.get('[data-testid="try-again"]').trigger('click')
    expect(w.emitted('retry')).toHaveLength(1)
  })

  it('disables the button and says "Trying…" while a try runs', () => {
    const w = mountIt({ trying: true, lastTriedAt: NOW })
    const b = w.get('[data-testid="try-again"]')
    expect(b.attributes('disabled')).toBeDefined()
    expect(b.text()).toBe('Trying…')
  })

  it('says "just now", then whole minutes, and updates calmly', async () => {
    const w = mountIt({ trying: false, lastTriedAt: NOW })
    expect(w.get('[data-testid="last-tried"]').text()).toBe('Last tried just now')
    await vi.advanceTimersByTimeAsync(45000)
    expect(w.get('[data-testid="last-tried"]').text()).toBe('Last tried just now')
    await vi.advanceTimersByTimeAsync(3 * 60000)
    expect(w.get('[data-testid="last-tried"]').text()).toBe('Last tried 3 min. ago')
  })

  it('names a running try in German as a state, not as a command', () => {
    setLocale('de')
    expect(mountIt({ trying: true, lastTriedAt: NOW }).get('[data-testid="try-again"]').text()).toBe('Wird versucht …')
  })

  it('speaks German and shows no last-try line before the first try', () => {
    setLocale('de')
    const w = mountIt({ trying: false, lastTriedAt: NOW })
    expect(w.get('[data-testid="last-tried"]').text()).toBe('Zuletzt versucht: gerade eben')
    expect(w.get('[data-testid="try-again"]').text()).toBe('Erneut versuchen')
    expect(mountIt({ trying: false, lastTriedAt: null }).find('[data-testid="last-tried"]').exists()).toBe(false)
  })
})
