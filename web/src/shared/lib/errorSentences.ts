import { ROWS as A } from './errorSentencesA'
import { ROWS as B } from './errorSentencesB'
import { ROWS as C } from './errorSentencesC'

// Every sentence the server puts in an error body, with the key of its translation under errors.detail.
// One row per sentence in internal/repo/testdata/error_messages.golden.json (regenerate that with
// `go test ./internal/repo -run ErrorMessageCatalogue -update`). errorDetails.test.ts fails for a sentence
// without a row, for a row whose sentence is gone, and when a sentence is answered by another row.
//
// %d, %s and %q stand for the parts that vary. The optional third entry names them in order for the
// translation (default {a}, {b}, {c}); the optional fourth gives fixed values, a field name that is
// shown by its label (errors.field). "time" is shown as a duration. The rows are kept in
// errorSentencesA/B/C.ts to stay under the file length limit.
export type SentenceRow = readonly [format: string, key: string, names?: readonly string[], fixed?: Readonly<Record<string, string>>]

export const SENTENCES: readonly SentenceRow[] = [...A, ...B, ...C]
