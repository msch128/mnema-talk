import { test } from 'node:test'
import assert from 'node:assert/strict'
import { Linter } from 'eslint'
import { testFunctionLength } from './testFunctionLength.ts'

function lint(code: string, max = 3): number {
  const linter = new Linter()
  const messages = linter.verify(code, [{
    plugins: { mnema: { rules: { 'test-function-length': testFunctionLength } } },
    rules: { 'mnema/test-function-length': ['error', { max }] }
  }])
  return messages.filter(m => m.ruleId === 'mnema/test-function-length').length
}

const BODY = '  a()\n  b()\n  c()\n  d()\n'

test('a test, a hook and a helper over the limit are reported', () => {
  assert.equal(lint(`it('x', () => {\n${BODY}})`), 1)
  assert.equal(lint(`beforeEach(() => {\n${BODY}})`), 1)
  assert.equal(lint(`function helper() {\n${BODY}}`), 1)
})

test('a function within the limit passes', () => {
  assert.equal(lint('it(\'x\', () => {\n  a()\n})'), 0)
})

test('a describe callback is exempt, in all its spellings, and the tests inside it are not', () => {
  assert.equal(lint(`describe('x', () => {\n${BODY}})`), 0)
  assert.equal(lint(`describe.skip('x', () => {\n${BODY}})`), 0)
  assert.equal(lint(`describe.skipIf(flag)('x', () => {\n${BODY}})`), 0)
  assert.equal(lint(`describe.each(rows)('x', () => {\n${BODY}})`), 0)
  assert.equal(lint(`describe('x', () => {\n  it('y', () => {\n${BODY}  })\n})`), 1)
})
