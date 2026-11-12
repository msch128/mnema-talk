// One rule, two implementations: which link targets the client links
// (safeHref, link.ts and linkAuthority.ts). The server's plain form ports it
// (internal/mdplain/href.go, host.go), since whether something is a link can
// move a spoiler. This file records the client's answers for the server's
// test (internal/mdplain/href_test.go); it fails when the client answers
// otherwise, so a change here cannot leave the port behind unnoticed.
// HREF_UPDATE=1 rewrites the file.
import { describe, expect, it } from 'vitest'
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { safeHref } from './link'

const HREF_FILE = join(process.cwd(), '..', 'internal', 'mdplain', 'testdata', 'hrefs.json')

const HOSTS = [
  // The URL standard's host rules.
  'a..b/', 'x_y.example', "a!$&'()*+,;=~{}.x/", '1.2.3.4/', '0x7f.1/', 'a.1e/', 'h.x:/', 'h.x:0080/', '.x/', 'x./', './',
  '-x-.y/', 'ab_.cd/', 'a.b_/', '0/', '1.2.3/', '4294967295/', '1.2.3.4./', 'A.B/', '{x}/', 'x.y/?a', 'x#y', 'x?y',
  'a.b.c.d.e.f/', '0x/', '0xg/', 'a.0xg/', '1.0x/', 'a.b./', 'a..b..c', '..', 'a...', '0.0.0.0', '00/', '0x00000000001/',
  '1.2.0x3/', 'x.y:65535', '-/', '_/', '1.2.3.999/', '1.2.3.4.5/', 'a.0x/', 'a.08/', 'h.x:65536/', 'a|b/', 'a^b/', '09/',
  '4294967296/', 'a..1/', '1..2/', 'a.b:12a/', 'x:1:2/', '12345678901234567890/', '[[zq]]/', '[::1]x/', '[/', '[zz]/',
  'e.example[/', '[::1]/', '[::1]:80/', '[1:2]/', 'xn--abc/', 'xn--caf-dma.example/', 'x.XN--y/', 'caf\u00e9.example/',
  // User info: the client links none.
  'u:p@h.x/', '@h.x/', 'a@b@c/', 'a.b@c.d/', 'h.x@/', 'a:b@/', 'a@[::1]/', ':@h.x/', 'allowed.example@other.example/',
  'h.x/@path', 'h.x?@q', 'h.x#@f',
  // Percent escapes in the authority: decoded before the host is read.
  'a%41b/', '%41.example/', 'x%2Ey/', 'a%2541/', 'a%zz/', 'a%4/', 'a%/', 'a%C3/', 'a%C3%A9.example/', 'a%C0%80.example/',
  'a%ED%A0%80.example/', 'a%E2%80%8Bb.example/', 'a%00b/', 'x%3Ay/', 'a%40b.example/', 'a%5Bb/', 'h.x:%38%30/',
  'a%7Cb/', 'a%20b/', 'a%E2%80%AEb.example/', 'A%2eB/',
  // Control and format characters in the authority, raw.
  'a\u200bb.example/', 'a\u00adb.example/', 'a\u0085b/', 'a.example\u200e/', 'a\u2060b.example/', 'a\ufe0fb.example/',
  // ... and outside it.
  'a.example/\u200b', 'a.example/%E2%80%8B', 'a.example?\u00ad'
]

const HREFS = [
  ...HOSTS.flatMap(host => [`http://${host}`, `https://${host}`]),
  'HTTP://A.EXAMPLE', 'http:///x', 'http://', 'mailto:a@b.example', 'mailto:?x', 'mailto:#x', 'mailto:a%zz', 'ftp://a.example'
]

interface HrefVector { href: string; ok: boolean }

describe('which link targets the client links (shared with the server)', () => {
  it('the recorded answers are the client\'s', () => {
    const vectors: HrefVector[] = HREFS.map(href => ({ href, ok: safeHref(href) !== null }))
    const text = JSON.stringify(vectors, null, 1) + '\n'
    if (process.env.HREF_UPDATE) writeFileSync(HREF_FILE, text)
    expect(readFileSync(HREF_FILE, 'utf8'), 'run with HREF_UPDATE=1, then make internal/mdplain agree').toBe(text)
  })
})
