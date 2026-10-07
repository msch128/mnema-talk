export { requireValue as required } from './test-fixtures.fixture'

let streamSequence = 0
/** Complete MediaStream stand-in for store identity/lifecycle tests. It has
 * no encoded media; real playback is covered by browser E2E tests. */
export function streamFixture(): MediaStream {
  const tracks: MediaStreamTrack[] = []
  return Object.assign(new EventTarget(), {
    id: `synthetic-stream-${++streamSequence}`,
    active: false,
    onaddtrack: null,
    onremovetrack: null,
    addTrack(track: MediaStreamTrack) { if (!tracks.includes(track)) tracks.push(track) },
    removeTrack(track: MediaStreamTrack) { const index = tracks.indexOf(track); if (index >= 0) tracks.splice(index, 1) },
    getTracks: () => [...tracks],
    getAudioTracks: () => tracks.filter(track => track.kind === 'audio'),
    getVideoTracks: () => tracks.filter(track => track.kind === 'video'),
    getTrackById: (id: string) => tracks.find(track => track.id === id) ?? null,
    clone: () => streamFixture(),
  })
}
