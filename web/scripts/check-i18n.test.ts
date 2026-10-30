import { test } from 'node:test'
import assert from 'node:assert/strict'
import { fileURLToPath } from 'node:url'
import { looksLikeProse, missingFindings, parityFindings, scanScript, scanTree, scanVue, unusedFindings, usedKeys } from './check-i18n.ts'

const kinds = (source: string, file = 'Bad.vue') => (file.endsWith('.vue') ? scanVue(source, file) : scanScript(source, file)).map(f => `${f.kind}:${f.text}`)

const BAD_VUE = `<script setup lang="ts">
import { confirm } from '../lib/confirm'
const toasts = { error(_s: string) {} }
async function go(count: number, d: Date) {
  await confirm({ title: 'Delete', body: 'Do you want to join the conversation?', confirmLabel: 'Join' })
  toasts.error('Failed')
  const label = count === 1 ? 'item' : 'items'
  return label + d.toLocaleDateString() + new Intl.NumberFormat().format(1)
}
</script>
<template>
  <div title="Close" :aria-label="'Open menu'">
    Hello there {{ ok ? 'Yes' : 'No' }}
    <input placeholder="Search">
  </div>
</template>
`

test('a bad fixture trips every kind of check', () => {
  const found = kinds(BAD_VUE)
  for (const expected of [
    'attr:title="Close"', 'expr:Open menu', 'text:Hello there', 'expr:Yes', 'expr:No', 'attr:placeholder="Search"',
    'literal:Delete', 'literal:Do you want to join the conversation?', 'literal:Join', 'literal:Failed',
    "plural:count === 1 ? 'item' : 'items'", 'format:toLocaleDateString() without a locale', 'format:Intl.NumberFormat() without a locale'
  ]) assert.ok(found.includes(expected), `missing ${expected} in ${JSON.stringify(found)}`)
})

test('translated and technical text passes', () => {
  const clean = `<script setup lang="ts">
import { t } from '../i18n'
import { locale } from '../i18n'
const when = (d: Date) => d.toLocaleDateString(locale.value) + new Intl.NumberFormat(locale.value).format(1)
const n = (count: number) => t(count === 1 ? 'a.one' : 'a.many')
</script>
<template>
  <div :title="t('a.b')" aria-label="" class="px-2">
    <span>{{ $t('a.b') }} · {{ when(d) }} ms</span>
    <a href="https://example.org" placeholder="https://example.org/hook">Mnema Talk</a>
    <b>&nbsp;→</b>
  </div>
</template>
`
  assert.deepEqual(kinds(clean), [])
})

test('i18n-ignore opts one line out', () => {
  assert.deepEqual(kinds("const a = 'Some English sentence here.' // i18n-ignore: stored name", 'a.ts'), [])
  assert.deepEqual(kinds("const a = 'Some English sentence here.'", 'a.ts'), ['literal:Some English sentence here.'])
})

test('error messages are reported as errors, log and comparison strings are not', () => {
  assert.deepEqual(kinds("throw new Error('Something went wrong here')", 'a.ts'), ['error:Something went wrong here'])
  assert.deepEqual(kinds("console.warn('Something went wrong here'); if (x === 'Some Value Here') y()", 'a.ts'), [])
})

test('prose detection', () => {
  assert.equal(looksLikeProse('Do you want to join?'), true)
  assert.equal(looksLikeProse('Mnema Talk'), false)
  assert.equal(looksLikeProse('MIT OR Apache-2.0'), false)
  assert.equal(looksLikeProse('some-css-class another'), false)
  assert.equal(looksLikeProse('Speichern'), false)
})

test('key parity and missing keys', () => {
  const de = { a: { b: 'x', n: { one: 'e', other: 'm' } }, only: 'x' }
  const en = { a: { b: 'x', n: { one: 'e', other: 'm' } }, extra: 'x' }
  assert.deepEqual(parityFindings(de, en).map(f => `${f.file}:${f.text}`), ['en.json missing:only', 'de.json missing:extra'])
  const used = usedKeys({ 'A.vue': `t('a.b') $t("a.n") t('a.gone') t('only')` })
  assert.deepEqual(missingFindings(used, de, en).map(f => f.text), ['a.gone'])
  assert.deepEqual(unusedFindings({ 'A.vue': `t('a.b') t(\`a.n.\${x}\`)` }, de).map(f => f.text), ['only'])
})

test('the real locale files agree and every literal key is defined', () => {
  const found = scanTree(fileURLToPath(new URL('../src', import.meta.url))).filter(f => f.kind === 'parity' || f.kind === 'missing')
  assert.deepEqual(found, [])
})
