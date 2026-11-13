import { test, expect } from './fixtures'
import { auditClipping } from './clipping/auditor'

// The clipping auditor on small pages made for one rule each: what it must
// report, and the deliberate case beside it that it must leave alone. These
// pages are plain HTML; no server is involved.

const ellipsis = 'overflow:hidden;text-overflow:ellipsis;white-space:nowrap'

async function findings(page: import('@playwright/test').Page, html: string, prepare?: () => void) {
  await page.setViewportSize({ width: 600, height: 400 })
  await page.setContent(`<!doctype html><body style="margin:0;font:16px sans-serif">${html}</body>`)
  if (prepare) await page.evaluate(prepare)
  return page.evaluate(auditClipping, { tolerance: 1 })
}

const rules = (list: Array<{ rule: string; selector: string }>, selector: string) => list.filter(f => f.selector === selector).map(f => f.rule)

test('a name cut to a sliver is reported even when a label holds it in full', async ({ page }) => {
  const list = await findings(page, `
    <button aria-label="Connected · A Talk with a long name" style="width:300px;display:flex">
      <span>Connected</span><span id="sliver" style="display:block;width:6px;${ellipsis}">A Talk with a long name</span>
    </button>
    <button aria-label="A Talk with a long name" style="width:300px;display:flex">
      <span id="enough" style="display:block;width:80px;${ellipsis}">A Talk with a long name</span>
    </button>`)
  expect(rules(list, '#sliver')).toEqual(['sliver'])
  expect(rules(list, '#enough')).toEqual([])
})

test('text under aria-hidden is still drawn, so its box may not cut it', async ({ page }) => {
  const list = await findings(page, `
    <div id="hidden" aria-hidden="true" style="width:60px;overflow:hidden;white-space:nowrap">A visible duplicate that is cut</div>
    <div id="whole" aria-hidden="true" style="width:400px;overflow:hidden;white-space:nowrap">A visible duplicate</div>`)
  expect(rules(list, '#hidden')).toEqual(['overflow-hidden'])
  expect(rules(list, '#whole')).toEqual([])
})

test('a control under a sticky bar is covered unless scrolling brings it out', async ({ page }) => {
  const bar = (edge: string) => `<div style="position:sticky;${edge}:0;height:40px;background:#222">Bar</div>`
  const list = await findings(page, `<div style="display:flex;gap:8px">
    <div style="width:140px;height:200px;overflow:auto">
      ${bar('top')}<button id="under" style="display:block;margin-top:-30px;height:40px">Press</button>
    </div>
    <div style="width:140px;height:120px;overflow:auto">
      ${bar('top')}<button id="at-top" style="display:block;margin-top:-30px;height:40px">Press</button><div style="height:400px"></div>
    </div>
    <div class="scrolled" style="width:140px;height:120px;overflow:auto">
      ${bar('top')}<div style="height:50px"></div><button id="scrolled-under" style="display:block;height:40px">Press</button><div style="height:400px"></div>
    </div>
    <div style="width:140px;height:120px;overflow:auto">
      <div style="height:65px"></div><button id="above-footer" style="display:block;height:40px">Press</button><div style="height:400px"></div>${bar('bottom')}
    </div></div>`, () => { for (const el of document.querySelectorAll('.scrolled')) el.scrollTop = 40 })
  expect(rules(list, '#under')).toEqual(['covered'])
  // At the top of its list, scrolling up has nothing left to reveal.
  expect(rules(list, '#at-top')).toEqual(['covered'])
  expect(rules(list, '#scrolled-under')).toEqual([])
  expect(rules(list, '#above-footer')).toEqual([])
})

test('the full-text marker counts only with the title it promises', async ({ page }) => {
  const long = 'A channel name that does not fit into its row at all'
  const list = await findings(page, `
    <span id="marker" data-full-text style="display:block;width:120px;${ellipsis}">${long}</span>
    <span id="titled" data-full-text title="${long}" style="display:block;width:120px;${ellipsis}">${long}</span>`)
  expect(rules(list, '#marker')).toEqual(['truncated-without-full-text'])
  expect(rules(list, '#titled')).toEqual([])
})

test('a menu panel that runs past the window edge is reported, even with its items whole', async ({ page }) => {
  const list = await findings(page, `
    <div id="past" role="menu" style="position:fixed;left:20px;top:300px;width:200px;height:106px;padding-bottom:8px;background:#222">
      <button style="display:block;height:30px">Reply</button><button style="display:block;height:30px">Delete</button>
    </div>
    <div id="inside" role="menu" style="position:fixed;left:300px;top:200px;width:200px;height:100px;background:#222"></div>`)
  expect(rules(list, '#past')).toEqual(['outside-viewport'])
  expect(rules(list, '#inside')).toEqual([])
})
