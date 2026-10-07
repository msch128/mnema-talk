import { vi } from 'vitest'

/** Test doubles expose the actual track/stream contract without pretending partial objects are DOM instances. */
export class TestTrack extends EventTarget implements MediaStreamTrack {
  contentHint = ''
  enabled = true
  static nextId = 0
  id = `test-track-${++TestTrack.nextId}`
  label = 'synthetic test source'
  muted = false
  readyState: MediaStreamTrackState = 'live'
  onended: MediaStreamTrack['onended'] = null
  onmute: MediaStreamTrack['onmute'] = null
  onunmute: MediaStreamTrack['onunmute'] = null
  constructor(public kind = 'audio') { super() }
  stop = vi.fn(() => {})
  clone = vi.fn((): TestTrack => {
    const clone = new TestTrack(this.kind)
    clone.enabled = this.enabled
    return clone
  })
  applyConstraints = vi.fn(async (_constraints?: MediaTrackConstraints) => {})
  getCapabilities(): MediaTrackCapabilities { return {} }
  getConstraints(): MediaTrackConstraints { return {} }
  getSettings(): MediaTrackSettings { return {} }
}

export class TestStream extends EventTarget implements MediaStream {
  active = true
  id = 'synthetic-test-stream'
  onaddtrack: MediaStream['onaddtrack'] = null
  onremovetrack: MediaStream['onremovetrack'] = null
  constructor(private tracks: MediaStreamTrack[] = []) { super() }
  getTracks() { return [...this.tracks] }
  getAudioTracks() { return this.tracks.filter(track => track.kind === 'audio') }
  getVideoTracks() { return this.tracks.filter(track => track.kind === 'video') }
  addTrack(track: MediaStreamTrack) { this.tracks.push(track) }
  removeTrack(track: MediaStreamTrack) { this.tracks = this.tracks.filter(item => item !== track) }
  getTrackById(id: string) { return this.tracks.find(track => track.id === id) ?? null }
  clone() { return new TestStream(this.tracks.map(track => track.clone())) }
}

// Use the shared fixture precondition check across frontend tests.
export { requireValue as present } from './test-fixtures.fixture'
