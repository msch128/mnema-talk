import { readFileSync } from 'node:fs'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { bindFixtureRenderer, type FixtureEvents, type Frame } from './gaming-fixture/fixture'

afterEach(() => { document.body.replaceChildren(); document.body.className = '' })
function mount(overlay: boolean) {
  document.body.className = overlay ? 'native-overlay' : 'native-widget'
  document.body.innerHTML = '<section id="fixture-root"><ul id="members"><li>Old retained name</li></ul></section>'
  if (overlay) {
    const widget = document.createElement('aside')
    widget.id = 'overlay-widget'
    document.getElementById('fixture-root')!.append(widget)
  }
  let deliver: (event: { payload: Frame }) => void = () => { throw new Error('listener not installed') }
  const events: FixtureEvents = { listen: vi.fn(async (_, callback) => { deliver = callback; return () => {} }) }
  bindFixtureRenderer(document, events)
  return { deliver: (payload: Frame) => deliver({ payload }), events }
}
const member = { name: 'Fixture', speaking: false, muted: false, sharing: false }
describe('local gaming projection renderer', () => {
  it('both shipped native surfaces load the exported renderer as a local ES module', () => {
    for (const surface of ['widget', 'overlay']) {
      const html = readFileSync(`public/fixtures/${surface}.html`, 'utf8')
      const page = document.createElement('template')
      page.innerHTML = html
      const scripts = page.content.querySelectorAll('script')
      expect(scripts).toHaveLength(1)
      expect(scripts[0]!.getAttribute('type')).toBe('module')
      expect(scripts[0]!.getAttribute('src')).toBe('fixture.js')
      expect(scripts[0]!.textContent).toBe('')
    }
  })
  it('clears old contents immediately and after voice visibility loss', () => {
    const { deliver } = mount(false)
    expect(document.getElementById('fixture-root')!.hidden).toBe(true)
    expect(document.getElementById('members')!.textContent).toBe('')
    deliver({ passive_widget: true, overlay: false, overlay_widget: false, members: [member] })
    expect(document.getElementById('fixture-root')!.hidden).toBe(false)
    expect(document.getElementById('members')!.textContent).toContain('Fixture')
    deliver({ passive_widget: false, overlay: false, overlay_widget: false, members: [member] })
    expect(document.getElementById('members')!.children).toHaveLength(0)
  })
  it('renders names as text, limits membership and covers status combinations', () => {
    const { deliver } = mount(false)
    deliver({ passive_widget: true, overlay: false, overlay_widget: false, members: [
      { ...member, name: '<img src=x onerror=alert(1)>', speaking: true },
      { ...member, muted: true, sharing: true },
      ...Array.from({ length: 8 }, () => member)
    ] })
    expect(document.querySelector('img')).toBeNull()
    expect(document.querySelectorAll('li')).toHaveLength(8)
    expect(document.getElementById('members')!.textContent).toContain('<img src=x onerror=alert(1)>')
    expect(document.querySelector('.speaking .state')!.textContent).toBe('spricht')
    expect(document.querySelector('.muted .state')!.textContent).toBe('stumm · teilt')
  })
  it('overlay visibility and embedded widget preference stay separate', () => {
    const { deliver } = mount(true)
    deliver({ passive_widget: false, overlay: true, overlay_widget: false, members: [member] })
    expect(document.getElementById('fixture-root')!.hidden).toBe(false)
    expect(document.getElementById('overlay-widget')!.hidden).toBe(true)
    deliver({ passive_widget: false, overlay: true, overlay_widget: true, members: [member] })
    expect(document.getElementById('overlay-widget')!.hidden).toBe(false)
    deliver({ passive_widget: true, overlay: false, overlay_widget: false, members: [member] })
    expect(document.getElementById('fixture-root')!.hidden).toBe(true)
  })
  it('unavailable or rejected native listener clears retained projection', async () => {
    mount(true)
    bindFixtureRenderer(document)
    expect(document.getElementById('members')!.textContent).toBe('')
    bindFixtureRenderer(document, { listen: async () => { throw new Error('fixture listener unavailable') } })
    await Promise.resolve()
    expect(document.getElementById('fixture-root')!.hidden).toBe(true)
    expect(document.getElementById('overlay-widget')!.hidden).toBe(true)
  })
  it('a damaged local page cannot publish a projection', () => {
    document.body.replaceChildren()
    let deliver: (event: { payload: Frame }) => void = () => {}
    bindFixtureRenderer(document, { listen: async (_, callback) => { deliver = callback; return () => {} } })
    expect(() => deliver({ payload: { passive_widget: true, overlay: false, overlay_widget: false, members: [member] } })).not.toThrow()
    expect(document.querySelector('li')).toBeNull()
  })
})
