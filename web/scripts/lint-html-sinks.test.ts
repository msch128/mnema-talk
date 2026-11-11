// Vue's Trusted Types policy passes text through, so the lint rules are what
// keeps HTML strings out of the app. vue/no-v-html covers the directive; these
// are the other ways to reach innerHTML through Vue or the DOM.
import assert from 'node:assert/strict'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'
import { ESLint } from 'eslint'

const web = fileURLToPath(new URL('../', import.meta.url))
const eslint = new ESLint({ cwd: web, overrideConfigFile: fileURLToPath(new URL('../eslint.config.ts', import.meta.url)) })

async function htmlFindings(code: string, file: string): Promise<string[]> {
  const [result] = await eslint.lintText(code, { filePath: `${web}${file}` })
  return (result?.messages ?? []).filter(m => m.message.startsWith('Do not write HTML as a string')).map(m => m.ruleId ?? '')
}

const component = (template: string, script = '') => `<script setup lang="ts">\n${script}\n</script>\n<template>\n  ${template}\n</template>\n`

test('a bound innerHTML or outerHTML prop is refused in a template', async () => {
  for (const attribute of [':innerHTML="html"', ':inner-html="html"', '.innerHTML="html"', 'v-bind:innerHTML.prop="html"', ':outerHTML="html"', 'v-bind="{ innerHTML: html }"']) {
    const found = await htmlFindings(component(`<div ${attribute} />`, 'const html = "<b>x</b>"'), 'src/components/Sample.vue')
    assert.deepEqual(found, ['vue/no-restricted-syntax'], attribute)
  }
})

test('h() with innerHTML and a direct assignment are refused in script', async () => {
  for (const code of ["h('div', { innerHTML: html })", "h('div', { 'innerHTML': html })", "h('div', { outerHTML: html })", 'el.innerHTML = html', "el['outerHTML'] = html"]) {
    const source = `import { h } from 'vue'\ndeclare const el: HTMLElement\nconst html = '<b>x</b>'\nexport const made = () => { ${code}; return h }\n`
    assert.deepEqual(await htmlFindings(source, 'src/lib/sample.ts'), ['no-restricted-syntax'], code)
    assert.deepEqual(await htmlFindings(component('<div />', source.replace('export const', 'const')), 'src/components/Sample.vue'), ['no-restricted-syntax'], code)
  }
})

test('the desktop adapters are held to the rule too', async () => {
  assert.deepEqual(await htmlFindings("declare const el: HTMLElement\nel.innerHTML = '<b>x</b>'\n", 'src/lib/platform.ts'), ['no-restricted-syntax'])
})

test('reading innerHTML, textContent and ordinary bindings stay allowed', async () => {
  assert.deepEqual(await htmlFindings("declare const el: HTMLElement\nexport const text = el.innerHTML\nel.textContent = 'x'\n", 'src/lib/sample.ts'), [])
  assert.deepEqual(await htmlFindings(component('<div :title="html" :textContent="html" />', 'const html = "x"'), 'src/components/Sample.vue'), [])
})
