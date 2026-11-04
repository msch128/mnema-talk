import { describe, expect, it } from 'vitest'
import { applyEdit } from './wikiEditorActions'

describe('wiki editor actions', () => {
  it('wraps the selection and keeps it selected', () => {
    expect(applyEdit('say hello now', 4, 9, 'bold')).toEqual({ text: 'say **hello** now', start: 6, end: 11 })
    expect(applyEdit('x', 0, 1, 'italic').text).toBe('*x*')
    expect(applyEdit('x', 0, 1, 'code').text).toBe('`x`')
  })

  it('inserts a placeholder when nothing is selected', () => {
    expect(applyEdit('', 0, 0, 'bold')).toEqual({ text: '**bold**', start: 2, end: 6 })
    expect(applyEdit('', 0, 0, 'wikilink').text).toBe('[[Page]]')
    expect(applyEdit('see ', 4, 4, 'link').text).toBe('see [text](https://)')
  })

  it('prefixes every touched line and takes the prefix off again', () => {
    const first = applyEdit('one\ntwo\nthree', 2, 5, 'list')
    expect(first.text).toBe('- one\n- two\nthree')
    expect(applyEdit(first.text, first.start, first.end, 'list').text).toBe('one\ntwo\nthree')
    expect(applyEdit('title', 0, 0, 'heading').text).toBe('## title')
    expect(applyEdit('a\nb', 3, 3, 'quote').text).toBe('a\n> b')
  })
})
