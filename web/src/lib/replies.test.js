import { describe, it, expect } from 'vitest'
import { replySnippet, previewText, markPreviewEdited, markPreviewDeleted, REPLY_PREVIEW_LEN } from './replies'

describe('reply previews', () => {
  it('truncates by code points like the server', () => {
    expect(previewText(undefined)).toBe('')
    expect(replySnippet('a'.repeat(300))).toHaveLength(REPLY_PREVIEW_LEN)
    const emoji = '🔥'.repeat(250)
    expect(Array.from(replySnippet(emoji))).toHaveLength(REPLY_PREVIEW_LEN)
    expect(replySnippet(null)).toBe('')
    expect(previewText(' line1\n\nline2 ')).toBe('line1 line2')
  })

  it('renders previews as plain text without markdown markers', () => {
    expect(previewText('Ja, Lounge ist offen. **Screenshare** geht auch.')).toBe('Ja, Lounge ist offen. Screenshare geht auch.')
    expect(previewText('*kursiv* und _auch_ und ***beides*** und ~~weg~~')).toBe('kursiv und auch und beides und weg')
    expect(previewText('||Spoiler: das Update kommt morgen|| (#5)')).toBe('Spoiler (#5)')
    expect(previewText('Code: `docker compose up -d` **x**')).toBe('Code: docker compose up -d x')
    expect(previewText('`**nicht fett**`')).toBe('**nicht fett**')
    expect(previewText('```js\nconst a = 1\n```')).toBe('const a = 1')
    expect(previewText('> zitat\nantwort')).toBe('zitat antwort')
    expect(previewText('siehe https://example.com/a_b_c')).toBe('siehe https://example.com/a_b_c')
    expect(previewText('snake_case_name bleibt')).toBe('snake_case_name bleibt')
  })

  it('updates previews of loaded replies on edit and delete', () => {
    const roots = [
      { id: 'o', content: 'old' },
      { id: 'r1', reply_to_id: 'o', reply_to: { id: 'o', content: 'old', deleted: false } }
    ]
    const thread = [{ id: 'r2', reply_to_id: 'o', reply_to: { id: 'o', content: 'old', deleted: false } }]
    const other = { id: 'r3', reply_to_id: 'x', reply_to: { id: 'x', content: 'keep' } }
    roots.push(other)

    expect(markPreviewEdited([roots, thread], { id: 'o', content: 'n'.repeat(500) })).toBe(2)
    expect(roots[1].reply_to.content).toHaveLength(REPLY_PREVIEW_LEN)
    expect(thread[0].reply_to.content).toHaveLength(REPLY_PREVIEW_LEN)
    expect(other.reply_to.content).toBe('keep')

    markPreviewDeleted([roots, thread], 'o')
    expect(roots[1].reply_to).toMatchObject({ deleted: true, content: '' })
    expect(thread[0].reply_to.deleted).toBe(true)
    expect(other.reply_to.deleted).toBeUndefined()
  })
})
