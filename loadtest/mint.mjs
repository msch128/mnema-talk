// Mints session tokens for the load-test users: HS256 JWTs with the claims of
// internal/auth/token.go IssueToken. Reads "id,token_version" lines on stdin
// (psql -At -F, output) and prints [{id, token}] as JSON.
//
//   JWT_SECRET=... node loadtest/mint.mjs < ids.csv > tokens.json
//
// Never commit the output: the tokens are valid sessions.
import { createHmac } from 'node:crypto'
import { readFileSync } from 'node:fs'

const secret = process.env.JWT_SECRET
if (!secret) throw new Error('JWT_SECRET is required')
const ttl = Number(process.env.TTL_S || 8 * 3600)
const b64 = (o) => Buffer.from(JSON.stringify(o)).toString('base64url')

const now = Math.floor(Date.now() / 1000)
const out = readFileSync(0, 'utf8')
  .split('\n')
  .map((l) => l.trim())
  .filter(Boolean)
  .map((line) => {
    const [id, tv] = line.split(/[,|]/)
    const head = `${b64({ alg: 'HS256', typ: 'JWT' })}.${b64({ uid: id, tv: Number(tv), iss: 'mnema-talk', sub: id, iat: now, nbf: now, exp: now + ttl })}`
    const sig = createHmac('sha256', secret).update(head).digest('base64url')
    return { id, token: `${head}.${sig}` }
  })
process.stdout.write(JSON.stringify(out))
