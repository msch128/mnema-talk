// The hand-written messages of the link parity check (linkParity.test.ts).
// The rules: an allow list of allowed.example, a block list of other.example,
// block-all, and the new-account rule (which blocks what block-all blocks).

/**
 * Messages the client shows with a link: no rule mode may let that link
 * through. The groups were the bypasses found by the 2026-11 hunt (H-L3-01 to
 * H-L3-04) and in their review; the server once judged another host than the
 * one the client opens, or none.
 */
export const AGREED: readonly string[] = [
  // Ordinary links.
  'https://other.example/x', 'see https://allowed.example@other.example/x', 'https://other.example.', '(https://other.example)',
  '[docs](https://other.example)', '<https://other.example>', '**https://other.example/x**', 'HTTPS://OTHER.EXAMPLE',
  'https://other.example\u200b', 'https://allowed.example\\@other.example', 'https://allowed.example`@other.example',
  'https://caf\u00e9.example', 'mailto:x@other.example', '`https://other.example`', "https://other.example's page",
  'https://allowed.example"@other.example/y', 'https://allowed.example\u3000@other.example/x',
  // H-L3-01: a character the server ended the authority at, the browser reads it as user info.
  "https://allowed.example'@other.example/x", 'https://allowed.example\uff02@other.example/x', 'https://allowed.example\uff07@other.example/x',
  'https://allowed.example\uff0f@other.example/x', 'https://allowed.example\uff1c@other.example/x', 'https://allowed.example\uff1e@other.example/x',
  'https://allowed.example\uff1f@other.example/x', 'https://allowed.example\uff03@other.example/x', 'https://allowed.example\uff3c@other.example/x',
  'https://allowed.example\uff40@other.example/x', "[docs](https://allowed.example'@other.example/x)", "<https://allowed.example'@other.example/x>",
  "https://'@other.example/x", 'https://\u0085@other.example/x', 'see https://\uff02@other.example/x',
  // H-L3-02: the client ends the link (or its host) before the server did.
  '**https://other.example**', '||https://other.example||', '~~https://other.example~~', '__https://other.example__', '*https://other.example*',
  'https://other.example|', '[docs](https://other.example)s', '**https://other.example**.allowed.example', '__https://other.example__.allowed.example',
  '~~https://other.example~~.allowed.example', '||https://other.example||.allowed.example', '[docs](https://other.example).allowed.example',
  'https://other.example\u007f@allowed.example', 'https://other.example\u0008@allowed.example/y', 'https://other.example\ufeff.allowed.example',
  'h\u0000ttps://other.example', 'http://_.other.example', 'http://a-.other.example/x', 'http://-a.other.example/x',
  // H-L3-03: the client rejects the outer link and links one inside it.
  'https://allowed.example:99999/https://other.example', 'https://allowed.example:70000/https://other.example/z', 'https://[::1/https://other.example',
  'www.allowed.example/r/https://other.example/x', 'https://allowed.example/r?u=https://other.example/x',
  // H-L3-04: a mark or format character the browser keeps in the host.
  'https://allowed\u0301.example', 'https://allowed.exampl\u0302e', 'https://allowed\u030a.example/q', 'https://allowed\u00ad.example',
  // Hosts a browser reads differently from Go's net/url.
  'https://0x7f.1/x', 'https://2130706433/x', 'https://other%2eexample/x', 'https://%6fther.example/x', 'https://other\uff0eexample/x',
  'https://other\u3002example/x', 'https://OTHER.example:/x', 'https://[0:0::1]/x', 'https://user:pw@other.example:443/x',
  // Found by the deep run of the fix: a cut right after the "]" of an IPv6 literal.
  '__http://ALLOWED.EXAMPLE_@[::1]__.allowed.example__', 'x*HTTPS://[0:0::1]\'*@allowed.example.:443/a(b)* now',
  "'__http://1.@2.3.25)6_@[::1]:__.allowed.example__",
  // Found by the nightly deep run: a label that is only the ACE prefix, which some URL parsers keep.
  'https://allowed.example.xn--/x', 'https://xn--.allowed.example.xn--/x', 'x<http://127.1\uff40_@xn--?caf-dma.example:70000#f>"',
  '[https://allowed.example](www.caf\u00e9.example\uff40\ufe65@xn--\u007fcaf-dma.example:443?q)',
  '"[https://allowed.example](www.user@xn--\u00a0caf-dma.exam\u0000ple:#f) now'
]

/** Links to allowed.example only: the allow list and the block list must let these through. */
export const PASSING: readonly string[] = [
  'https://allowed.example/x', 'see https://docs.allowed.example/guide?a=1#b.', '(https://allowed.example)', '[docs](https://allowed.example/x)',
  '<https://allowed.example>', '**https://allowed.example**', '*https://allowed.example*', '||https://allowed.example||',
  '~~https://allowed.example~~', '__https://allowed.example__', 'https://allowed.example!', 'https://allowed.example:8080/x',
  'https://ALLOWED.EXAMPLE./x', "https://allowed.example's page", 'www.allowed.example', 'https://allowed.example/a_(b)',
  'https://my_host.allowed.example/x', 'https://caf\u00e9.allowed.example/x', '[https://allowed.example](https://allowed.example/x).',
  'https://allowed.example?', '"https://allowed.example", he said'
]
