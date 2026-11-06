import { beforeEach, describe, expect, it } from 'vitest'
import { setLocale, t } from './index'

describe('counted messages', () => {
  beforeEach(() => setLocale('en'))

  it.each([
    ['modHistory.strikeCount', 1, '1 strike'],
    ['modHistory.strikeCount', 3, '3 strikes'],
    ['dm.unreadTotal', 1, '1 unread direct message'],
    ['sidebar.categoryMentions', 1, '1 mention in this category'],
    ['moderation.bulkDeleted', 1, '1 message from Ada deleted.'],
    ['security.twoFactor.notice', 1, '1 wrong code was entered for your account since you last signed in.'],
    ['wiki.subpageCount', 2, '2 subpages']
  ])('%s with %i reads in the right number', (key, count, text) => {
    expect(t(key, { count, name: 'Ada' })).toBe(text)
  })

  it('uses the German singular', () => {
    setLocale('de')
    expect(t('modHistory.strikeCount', { count: 1 })).toBe('1 Verwarnung')
  })
})
