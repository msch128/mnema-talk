// What makes two wiki titles "the same page" (doc/wiki.md), the same rule as
// the server's titleKey (internal/wiki/title_key.go): the title decomposed
// (NFD), every character lower-cased on its own, composed again (NFC). One
// character at a time keeps toLowerCase() to the simple mapping: no final sigma
// (it needs a word around it), and after NFD no U+0130 is left, the one letter
// whose full lower case is two characters. Both sides are checked against
// internal/wiki/testdata/title_keys.json.

/** The key two titles share exactly when they name one page. */
export function wikiTitleKey(title: string): string {
  let out = ''
  for (const ch of title.normalize('NFD')) out += ch.toLowerCase()
  return out.normalize('NFC')
}
