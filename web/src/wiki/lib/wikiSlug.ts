// The address a title would get (the server derives it the same way, and adds
// -2, -3 … when it is taken); shown as the placeholder of the address field.

const MAX_SLUG = 80
const LETTERS: Record<string, string> = {
  ä: 'ae', ö: 'oe', ü: 'ue', ß: 'ss', æ: 'ae', ø: 'o', å: 'a', é: 'e', è: 'e', ê: 'e', ë: 'e', á: 'a', à: 'a', â: 'a', ã: 'a',
  í: 'i', ì: 'i', î: 'i', ï: 'i', ó: 'o', ò: 'o', ô: 'o', õ: 'o', ú: 'u', ù: 'u', û: 'u', ñ: 'n', ç: 'c', ł: 'l', ś: 's',
  š: 's', ž: 'z', ý: 'y', ÿ: 'y', ź: 'z', ż: 'z', ć: 'c', ń: 'n', ą: 'a', ę: 'e', č: 'c', ř: 'r', ě: 'e', ő: 'o', ű: 'u'
}

export function deriveSlug(title: string): string {
  let out = ''
  let gap = false
  for (const ch of title.toLowerCase()) {
    const part = /[a-z0-9]/.test(ch) ? ch : (LETTERS[ch] ?? '')
    if (!part) {
      // Letters and digits of other scripts, spaces and punctuation separate; anything else is dropped.
      gap = gap || (out !== '' && /[\p{L}\p{N}\p{P}\s_-]/u.test(ch))
      continue
    }
    if (gap) out += '-'
    gap = false
    out += part
  }
  const cut = out.length > MAX_SLUG ? out.slice(0, MAX_SLUG).replace(/-+$/, '') : out
  return cut || 'page'
}
