// The save model of doc/settings-ux.md, checked where it can be checked by
// reading the source: a screen is either immediate or a draft, never both.
import { describe, it, expect } from 'vitest'

const components = import.meta.glob<string>(['../../components/*.vue', '../../*/components/*.vue'], { query: '?raw', import: 'default', eager: true })
const files = Object.entries(components).map(([path, source]) => ({ name: path.replace(/^.*\/components\//, ''), source }))

const isDraft = (source: string) => /\buseSettingsDraft\s*[<(]/.test(source)
const usesImmediate = (source: string) => /\busePreferenceSave\s*\(|<SavedIndicator\b/.test(source)
const pathsWith = (pick: (source: string) => boolean) => files.filter(f => pick(f.source)).map(f => f.name)

describe('the save model', () => {
  it('finds the screens it checks', () => {
    expect(pathsWith(isDraft).length).toBeGreaterThan(10)
    expect(pathsWith(usesImmediate).length).toBeGreaterThan(3)
  })

  it('never mixes a draft with immediate saving in one component', () => {
    expect(pathsWith(s => isDraft(s) && usesImmediate(s))).toEqual([])
    expect(pathsWith(s => /<UnsavedChangesBar\b/.test(s) && usesImmediate(s))).toEqual([])
  })

  it('stages an image picked on a draft screen instead of uploading it at once', () => {
    expect(pathsWith(s => isDraft(s) && /type="file"/.test(s) && !/\buseStagedImage\s*\(/.test(s))).toEqual([])
  })

  it('tracks every unsaved bar with a draft or a leave guard', () => {
    const tracked = (s: string) => isDraft(s) || /\buseLeaveGuard\s*\(/.test(s) || /\bdefineProps\b[\s\S]*\bdirty\b/.test(s)
    expect(pathsWith(s => /<UnsavedChangesBar\b/.test(s) && !tracked(s))).toEqual([])
  })
})
