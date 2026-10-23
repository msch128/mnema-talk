import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { PINNED_VERSION, patchEmojiPicker } from './emojiPickerCsp.ts'

const dir = new URL('../node_modules/emoji-picker-element/', import.meta.url)
const source = readFileSync(new URL('picker.js', dir), 'utf8')

test('the picker is pinned to the version the patch was written for', () => {
  const own = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8')) as { dependencies: Record<string, string> }
  assert.equal(own.dependencies['emoji-picker-element'], PINNED_VERSION)
  assert.equal((JSON.parse(readFileSync(new URL('package.json', dir), 'utf8')) as { version: string }).version, PINNED_VERSION)
})

test('the patch moves the base styles and style attributes off the inline paths', () => {
  const patched = patchEmojiPicker(source)
  assert.match(patched, /adoptedStyleSheets = \[sheet\]/)
  assert.match(patched, /targetNode\.style\.cssText = newValue/)
  assert.equal(patched.match(/setAttribute\(attributeName, newValue\)/g)?.length, 1)
})

test('the patch refuses a package it does not recognise', () => {
  assert.throws(() => patchEmojiPicker('// something else'), /exactly one match/)
  assert.throws(() => patchEmojiPicker(source + source), /exactly one match/)
})
