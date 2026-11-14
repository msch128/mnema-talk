import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MessageAttachments from './MessageAttachments.vue'
import { setLocale, t } from '../../i18n'
import { fixtureId } from '../../test-fixtures.fixture'
import type { MediaAttachment } from '../../types/domain'

// A removed file shows the "File removed" tile where it was, for pictures,
// videos and other files alike (doc/timed-messages.md, "Removed files"): a
// picture keeps its measured size, nothing links to the gone file, and the
// tile is named for screen readers.

enableAutoUnmount(afterEach)

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
})

const file = (over: Partial<MediaAttachment>): MediaAttachment => ({
  id: fixtureId(40), url: `/api/media/${fixtureId(40)}`, original_filename: 'plan.pdf', mime_type: 'application/pdf', size_bytes: 2048, is_deleted: true, ...over
})

describe('the removed-file tile', () => {
  it('takes the place and the measured size of a removed picture', () => {
    const w = mount(MessageAttachments, { props: { attachments: [file({ original_filename: 'wide.png', mime_type: 'image/png', width: 1600, height: 400 })] } })
    const tile = w.get('[data-media-placeholder="gone"]')
    expect(tile.attributes('role')).toBe('img')
    expect(tile.attributes('aria-label')).toBe('File removed: wide.png')
    expect(tile.text()).toBe('File removed')
    const style = tile.attributes('style') ?? ''
    expect(style).toContain('width: 448px')
    expect(style).toContain('aspect-ratio: 1600 / 400')
  })

  it('stands in for a removed video, and keeps a removed file’s row, without a link to either', () => {
    const w = mount(MessageAttachments, {
      props: { attachments: [file({ id: fixtureId(41), original_filename: 'clip.mp4', mime_type: 'video/mp4' }), file({ id: fixtureId(42) })] }
    })
    const tiles = w.findAll('[data-media-placeholder="gone"]')
    expect(tiles.map(tile => tile.attributes('aria-label'))).toEqual(['File removed: clip.mp4'])
    expect(w.get('[data-attachment="file"]').text()).toContain('plan.pdf')
    expect(w.find('video').exists()).toBe(false)
    expect(w.find('a[href]').exists()).toBe(false)
  })

  it('speaks German', () => {
    setLocale('de')
    const w = mount(MessageAttachments, { props: { attachments: [file({ mime_type: 'image/png', original_filename: 'a.png' })] } })
    const text = w.get('[data-media-placeholder="gone"]').text()
    expect(text).toBe(t('media.fileRemoved'))
    expect(text).not.toBe('File removed')
  })
})
