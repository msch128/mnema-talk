// The 80-line function limit for test code. A test (it, test), a hook, a
// helper or a fixture builder is a function like any other and obeys it. A
// describe callback is not logic but a list of cases that share a name, and
// its size is bounded by the 800-line file limit; it is the one exemption.
import type { Rule } from 'eslint'

export const SUITE_NAMES = new Set(['describe', 'suite'])

type Callee = { type: string; name?: string; object?: Callee; callee?: Callee }

/** describe, describe.skip, describe.each(table), describe.skipIf(x), suite... */
export function isSuiteCallee(callee: Callee | undefined): boolean {
  let node = callee
  while (node) {
    if (node.type === 'Identifier') return SUITE_NAMES.has(node.name ?? '')
    if (node.type === 'MemberExpression' || node.type === 'ChainExpression') node = node.object
    else if (node.type === 'CallExpression') node = node.callee
    else return false
  }
  return false
}

export const testFunctionLength: Rule.RuleModule = {
  meta: {
    type: 'problem',
    schema: [{ type: 'object', properties: { max: { type: 'number' } }, additionalProperties: false }],
    messages: { tooLong: 'This test function has {{lines}} lines, the limit is {{max}}: extract helpers (a describe callback is exempt).' }
  },
  create(context) {
    const options = context.options[0] as { max?: number } | undefined
    const max = options?.max ?? 80
    const check = (node: Rule.Node) => {
      const parent = node.parent as { type: string; callee?: Callee; arguments?: unknown[] } | undefined
      if (parent?.type === 'CallExpression' && parent.arguments?.includes(node) && isSuiteCallee(parent.callee)) return
      const lines = (node.loc?.end.line ?? 0) - (node.loc?.start.line ?? 0) + 1
      if (lines > max) context.report({ node, messageId: 'tooLong', data: { lines: String(lines), max: String(max) } })
    }
    return { FunctionDeclaration: check, FunctionExpression: check, ArrowFunctionExpression: check }
  }
}
