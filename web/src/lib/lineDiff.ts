// A small line diff for the revision view: what changed between two texts, by
// line. The longest common subsequence of the lines decides what stays; the
// rest is shown as removed and added. Texts are at most 50 KB, a few hundred
// lines, so the table fits easily; a very long pair is cut off instead of
// computed (the caller shows both texts then).

export type DiffOp = { kind: 'same' | 'add' | 'del'; text: string }

/** More lines than this on both sides at once and the diff is not computed. */
export const MAX_DIFF_CELLS = 4_000_000

/** The lines of a text; one trailing newline does not make an extra line. */
function lines(text: string): string[] {
  if (text === '') return []
  const parts = text.split('\n')
  if (parts.at(-1) === '') parts.pop()
  return parts
}

/** The diff from `before` to `after`, or null when the texts are too large to compare line by line. */
export function lineDiff(before: string, after: string): DiffOp[] | null {
  const a = lines(before)
  const b = lines(after)
  // Common start and end need no table.
  let head = 0
  while (head < a.length && head < b.length && a[head] === b[head]) head++
  let tail = 0
  while (tail < a.length - head && tail < b.length - head && a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail++
  const midA = a.slice(head, a.length - tail)
  const midB = b.slice(head, b.length - tail)
  if ((midA.length + 1) * (midB.length + 1) > MAX_DIFF_CELLS) return null
  return [
    ...a.slice(0, head).map(text => ({ kind: 'same' as const, text })),
    ...middle(midA, midB),
    ...a.slice(a.length - tail).map(text => ({ kind: 'same' as const, text }))
  ]
}

const cell = (table: Uint32Array, index: number): number => table[index] ?? 0
const lineAt = (list: readonly string[], index: number): string => list[index] ?? ''

function middle(a: readonly string[], b: readonly string[]): DiffOp[] {
  const rows = a.length + 1
  const cols = b.length + 1
  const table = new Uint32Array(rows * cols)
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      table[i * cols + j] = a[i] === b[j]
        ? cell(table, (i + 1) * cols + j + 1) + 1
        : Math.max(cell(table, (i + 1) * cols + j), cell(table, i * cols + j + 1))
    }
  }
  const out: DiffOp[] = []
  let i = 0
  let j = 0
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      out.push({ kind: 'same', text: lineAt(a, i) })
      i++
      j++
    } else if (cell(table, (i + 1) * cols + j) >= cell(table, i * cols + j + 1)) {
      out.push({ kind: 'del', text: lineAt(a, i) })
      i++
    } else {
      out.push({ kind: 'add', text: lineAt(b, j) })
      j++
    }
  }
  for (; i < a.length; i++) out.push({ kind: 'del', text: lineAt(a, i) })
  for (; j < b.length; j++) out.push({ kind: 'add', text: lineAt(b, j) })
  return out
}

/** How many lines were added and removed. */
export function diffStats(ops: readonly DiffOp[]): { added: number; removed: number } {
  let added = 0
  let removed = 0
  for (const op of ops) {
    if (op.kind === 'add') added++
    else if (op.kind === 'del') removed++
  }
  return { added, removed }
}
