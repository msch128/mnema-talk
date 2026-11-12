// The architecture page must not credit the server with a check it does not make
// (H-L2-01): the relay checks framing and room membership, members vet commits.
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { expect, it } from 'vitest'

const root = join(import.meta.dirname, '..', '..')

it('doc/architecture.md: the server does not validate who may commit', () => {
  const text = readFileSync(join(root, 'doc/architecture.md'), 'utf8').replace(/\s+/g, ' ')
  expect(text).not.toContain('The server validates who may commit')
  expect(text).toContain('the members validate who may commit')
})
