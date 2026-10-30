import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { requireValue } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { TrayItem } from '../lib/attachmentTray'
import AttachmentTray from './AttachmentTray.vue'

function item(over: Partial<TrayItem> & { name?: string; size?: number; type?: string } = {}): TrayItem {
  const file = new File(['x'], over.name ?? 'cat.png', { type: over.type ?? 'image/png' })
  Object.defineProperty(file, 'size', { value: over.size ?? 2048 })
  return {
    key: over.key ?? 'k1', file, channelId: 'ch1', kind: over.kind ?? 'image', previewUrl: 'blob:cat',
    status: 'ready', progress: 1, attachmentId: 'a1', stagedAt: 0, error: null, ...over
  }
}

const show = (items: TrayItem[]) => mount(AttachmentTray, { props: { items } })

beforeEach(() => setLocale('en'))

describe('AttachmentTray', () => {
  it('renders nothing without files', () => {
    expect(show([]).find('[data-testid="attachment-tray"]').exists()).toBe(false)
  })

  it('shows each file with its picture, name and size, and counts them', () => {
    const w = show([item({ key: 'a', name: 'cat.png' }), item({ key: 'b', name: 'clip.mp4', kind: 'video', type: 'video/mp4', size: 3 * 1024 * 1024 }), item({ key: 'c', name: 'notes.pdf', kind: 'file', type: 'application/pdf' })])
    const cards = w.findAll('li')
    expect(cards).toHaveLength(3)
    expect(cards.map(c => c.attributes('data-attachment-kind'))).toEqual(['image', 'video', 'file'])
    expect(requireValue(cards[0]).find('img').attributes('src')).toBe('blob:cat')
    expect(requireValue(cards[1]).find('video').exists()).toBe(true)
    expect(requireValue(cards[2]).find('img, video').exists()).toBe(false)
    expect(requireValue(cards[1]).text()).toContain('clip.mp4')
    expect(requireValue(cards[1]).text()).toContain('3')
    expect(w.text()).toContain('3 of 10 files')
  })

  it('shows the progress of an upload for assistive technology', () => {
    const w = show([item({ status: 'uploading', progress: 0.4 })])
    const bar = w.find('[role="progressbar"]')
    expect(bar.attributes('aria-valuenow')).toBe('40')
    expect(bar.attributes('aria-label')).toBe('Uploading cat.png')
    expect(w.find('[role="status"]').text()).toBe('Uploading…')
  })

  it('names a failed upload and offers a retry', async () => {
    const w = show([item({ status: 'error', error: 'File type is not allowed' })])
    expect(w.find('[data-testid="attachment-error"]').text()).toBe('File type is not allowed')
    expect(w.find('[role="status"]').text()).toBe('Upload failed')
    await w.find('[data-attachment-status="error"] button').trigger('click')
    expect(w.emitted('retry')).toEqual([['k1']])
  })

  it('removes and views a file by its own buttons', async () => {
    const w = show([item({ key: 'a', name: 'cat.png' }), item({ key: 'b', name: 'doc.pdf', kind: 'file' })])
    await w.find('[aria-label="Remove cat.png"]').trigger('click')
    await w.find('[aria-label="View cat.png"]').trigger('click')
    expect(w.emitted('remove')).toEqual([['a']])
    expect(w.emitted('view')).toEqual([['a']])
    expect(w.find('[aria-label="View doc.pdf"]').exists()).toBe(false)
    expect(w.find('[aria-label="Remove doc.pdf"]').exists()).toBe(true)
  })

  it('says everything is ready once all uploads finished', () => {
    expect(show([item()]).find('[role="status"]').text()).toBe('Ready to send')
  })
})
