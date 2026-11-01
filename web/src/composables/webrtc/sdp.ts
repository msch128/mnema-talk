/**
 * The SFU creates our four receive lines before anything else, so they are
 * the offer's first two audio lines (microphone, screen share's sound) and
 * first two video lines (screen, camera). Their direction says nothing: the
 * SFU may reuse them to forward other members' media (then they read
 * sendrecv/sendonly).
 */
export function publishMids(sdp: string | null | undefined, layerLines = 0, cameraLayerLines = 0) {
  const out: PublishMids = { audio: null, screenAudio: null, video: [], cameraLayers: [] }
  let kind: string | undefined | null = null
  let mid: string | null = null
  const flush = () => {
    if (kind && mid !== null) assignMid(out, kind, mid, 2 + layerLines + cameraLayerLines)
  }
  for (const line of String(sdp || '').split(/\r?\n/)) {
    if (line.startsWith('m=')) {
      flush()
      kind = line.slice(2).split(' ')[0]
      mid = null
    } else if (line.startsWith('a=mid:')) {
      mid = line.slice(6).trim()
    }
  }
  flush()
  // After the screen, the camera and the screen's lower layers come those of the camera.
  out.cameraLayers = out.video.splice(2 + layerLines)
  return out
}

export interface PublishMids { audio: string | null; screenAudio: string | null; video: string[]; cameraLayers: string[] }

// The first two audio lines are the microphone and the screen's sound, the
// first `videoLines` video lines the screen, camera and lower layers.
function assignMid(out: PublishMids, kind: string, mid: string, videoLines: number) {
  if (kind === 'audio') {
    if (out.audio === null) out.audio = mid
    else if (out.screenAudio === null) out.screenAudio = mid
  } else if (kind === 'video' && out.video.length < videoLines) {
    out.video.push(mid)
  }
}

// How a shared screen starts: the browser's bandwidth estimate opens at a few
// hundred kbit/s and only creeps up, which at 4K means a slideshow for the
// first half minute. The estimate starts at SCREEN_START_KBPS instead and
// never drops below SCREEN_MIN_KBPS (congestion control still backs off above
// that). Chrome reads both from the codec parameters of what it sends.
export const SCREEN_START_KBPS = 6000
export const SCREEN_MIN_KBPS = 1000

// Payload types that carry no video of their own.
const AUX_CODECS = new Set(['rtx', 'red', 'ulpfec', 'flexfec-03'])

// H.264 first, preferring constrained baseline (what the hardware encoders on
// Mac and Windows handle everywhere), then baseline, then other profiles.
function h264Rank(name: string, fmtp: string) {
  if (name !== 'h264') return 4
  if (!/packetization-mode=1/.test(fmtp)) return 3
  const profile = (/profile-level-id=([0-9a-f]{4})/i.exec(fmtp)?.[1] || '').toLowerCase()
  if (profile === '42e0') return 0
  if (profile === '4200') return 1
  return 2
}

// Sealed video needs framing that keeps what the network path parses readable,
// which exists for these codecs only (see lib/media/codec.ts).
const SEALABLE_VIDEO = new Set(['vp8', 'h264'])

/** [start, end) of every m= section of an SDP's lines. */
function sectionBounds(lines: string[]) {
  const starts: number[] = []
  lines.forEach((l, i) => { if (l.startsWith('m=')) starts.push(i) })
  return starts.map((from, n): [number, number] => [from, starts[n + 1] ?? lines.length])
}

/** payload type -> codec name (lower case) of a section's rtpmap lines. */
function codecNames(section: string[]) {
  const names = new Map<string, string>()
  for (const l of section) {
    const m = /^a=rtpmap:(\d+) ([^/]+)\//.exec(l)
    if (m?.[1] && m[2]) names.set(m[1], m[2].toLowerCase())
  }
  return names
}

/** payload type -> the payload type it retransmits (fmtp apt=). */
function retransmitTargets(section: string[]) {
  const apt = new Map<string, string>()
  for (const l of section) {
    const m = /^a=fmtp:(\d+) .*\bapt=(\d+)/.exec(l)
    if (m?.[1] && m[2]) apt.set(m[1], m[2])
  }
  return apt
}

function findSealablePts(names: Map<string, string>, apt: Map<string, string>): Set<string> {
  const keep = new Set<string>()
  for (const [pt, name] of names) {
    if (SEALABLE_VIDEO.has(name)) keep.add(pt)
  }
  for (const [pt, name] of names) {
    if (name === 'rtx' && keep.has(apt.get(pt) || '')) keep.add(pt)
  }
  return keep
}

function sealablePayloadTypes(section: string[]): { keep: Set<string>; listed: string[] } | null {
  const names = codecNames(section)
  const apt = retransmitTargets(section)
  const keep = findSealablePts(names, apt)
  const listed = section[0]?.split(' ').slice(3) || []
  if (!listed.some(pt => SEALABLE_VIDEO.has(names.get(pt) || ''))) return null
  return { keep, listed }
}

function filterRestrictedLine(l: string, keep: Set<string>, listed: string[]): string[] {
  if (l.startsWith('m=')) {
    return [[...l.split(' ').slice(0, 3), ...listed.filter(pt => keep.has(pt))].join(' ')]
  }
  const m = /^a=(?:rtpmap|fmtp|rtcp-fb):(\d+)\b/.exec(l)
  if (m?.[1] && !keep.has(m[1])) return []
  return [l]
}

/** The section without its unsealable codecs, or null if it needs no change. */
function restrictVideoSection(section: string[]) {
  if (!section[0]?.startsWith('m=video')) return null
  const sealable = sealablePayloadTypes(section)
  if (!sealable) return null
  return section.flatMap(l => filterRestrictedLine(l, sealable.keep, sealable.listed))
}

/**
 * Drops every video codec that cannot be sealed from the offer's video lines
 * (and the retransmission entries that point at them), so neither side ever
 * sends or expects one. A line that would be left without a codec stays as it
 * is: its frames are refused by the media worker instead.
 */
export function restrictVideoCodecs(sdp: string) {
  if (!sdp) return sdp
  const eol = sdp.includes('\r\n') ? '\r\n' : '\n'
  const lines = sdp.split(/\r?\n/)
  const bounds = sectionBounds(lines)
  for (let n = bounds.length - 1; n >= 0; n--) {
    const [from, to] = bounds[n] as [number, number]
    const out = restrictVideoSection(lines.slice(from, to))
    if (out) lines.splice(from, to - from, ...out)
  }
  return lines.join(eol)
}

/**
 * Tunes the screen line (mid) of the SFU's offer before it is applied: with
 * h264, H.264 moves to the front so the browser sends it (hardware encoded on
 * Mac and Windows; VP8 at 4K runs on the CPU and tops out near 30 fps), and
 * every video codec on the line gets the start and floor bitrate above. The
 * SFU forwards whatever arrives, so viewers decode the same codec.
 */
export function tuneScreenOffer(sdp: string, mid: string | null | undefined, { h264 = false, startKbps = SCREEN_START_KBPS, minKbps = SCREEN_MIN_KBPS } = {}) {
  if (!sdp || mid === null || mid === undefined) return sdp
  const eol = sdp.includes('\r\n') ? '\r\n' : '\n'
  const lines = sdp.split(/\r?\n/)
  for (const [from, to] of sectionBounds(lines)) {
    const section = lines.slice(from, to)
    if (!section[0]?.startsWith('m=video') || !section.includes(`a=mid:${mid}`)) continue
    lines.splice(from, to - from, ...tuneVideoSection(section, h264, startKbps, minKbps))
    break
  }
  return lines.join(eol)
}

/** payload type -> its fmtp parameters. */
function fmtpParams(section: string[]) {
  const fmtps = new Map<string, string>()
  for (const l of section) {
    const m = /^a=fmtp:(\d+) (.*)$/.exec(l)
    if (m?.[1] && m[2] !== undefined) fmtps.set(m[1], m[2])
  }
  return fmtps
}

interface TuneContext {
  names: Map<string, string>
  fmtps: Map<string, string>
  h264: boolean
  startKbps: number
  minKbps: number
}

/** Whether a payload type carries video itself (not retransmission or redundancy). */
const isMediaCodec = (c: TuneContext, pt: string) => c.names.has(pt) && !AUX_CODECS.has(c.names.get(pt) || '')

/** The m= line with the payload types in preference order: H.264 first. */
function preferH264(line: string, c: TuneContext) {
  const parts = line.split(' ')
  const pts = parts.slice(3)
  const rank = (pt: string) => (isMediaCodec(c, pt) ? h264Rank(c.names.get(pt) || '', c.fmtps.get(pt) || '') : 5)
  const sorted = pts.map((pt, i): [string, number] => [pt, i]).sort((a, b) => rank(a[0]) - rank(b[0]) || a[1] - b[1]).map(e => e[0])
  return [...parts.slice(0, 3), ...sorted].join(' ')
}

function tuneFmtpLine(l: string, c: TuneContext, bitrate: string): string[] | null {
  const fmtp = /^a=fmtp:(\d+) (.*)$/.exec(l)
  if (!fmtp?.[1] || fmtp[2] === undefined) return null
  if (!isMediaCodec(c, fmtp[1]) || fmtp[2].includes('x-google-')) return null
  return [`${l};${bitrate}`]
}

function tuneRtpmapLine(l: string, c: TuneContext, bitrate: string): string[] | null {
  const rtpmap = /^a=rtpmap:(\d+) /.exec(l)
  if (!rtpmap?.[1]) return null
  if (!isMediaCodec(c, rtpmap[1]) || c.fmtps.has(rtpmap[1])) return null
  return [l, `a=fmtp:${rtpmap[1]} ${bitrate}`]
}

/** One line of the section, with the start and floor bitrate added to the codecs that send video. */
function tuneLine(l: string, c: TuneContext): string[] {
  if (l.startsWith('m=') && c.h264) return [preferH264(l, c)]
  if (!c.startKbps) return [l]
  const bitrate = `x-google-start-bitrate=${c.startKbps};x-google-min-bitrate=${c.minKbps}`
  const tunedFmtp = tuneFmtpLine(l, c, bitrate)
  if (tunedFmtp) return tunedFmtp
  const tunedRtpmap = tuneRtpmapLine(l, c, bitrate)
  if (tunedRtpmap) return tunedRtpmap
  return [l]
}

function tuneVideoSection(section: string[], h264: boolean, startKbps: number, minKbps: number) {
  const c: TuneContext = { names: codecNames(section), fmtps: fmtpParams(section), h264, startKbps, minKbps }
  return section.flatMap(l => tuneLine(l, c))
}

/** tuneScreenOffer for the screen line and the lines of its lower layers. */
export function tuneScreenLines(sdp: string, mids: { video: string[] }, h264: boolean) {
  let tuned = sdp
  for (const mid of [mids.video[0], ...mids.video.slice(2)]) tuned = tuneScreenOffer(tuned, mid, { h264 })
  return tuned
}

/** One Opus fmtp line with discontinuous transmission requested (usedtx=1), whatever it said before. */
function withDtx(params: string) {
  const rest = params.split(';').filter(p => p.trim() && !/^\s*usedtx=/.test(p))
  return [...rest, 'usedtx=1'].join(';')
}

function dtxSection(section: string[]) {
  const opus = section.map(l => /^a=rtpmap:(\d+) opus\//i.exec(l)?.[1]).find(Boolean)
  if (!opus) return section
  const fmtp = new RegExp(`^a=fmtp:${opus} (.*)$`)
  if (section.some(l => fmtp.test(l))) return section.map(l => l.replace(fmtp, (_, params: string) => `a=fmtp:${opus} ${withDtx(params)}`))
  // Opus without parameters: the line goes right after its rtpmap.
  return section.flatMap(l => (l.startsWith(`a=rtpmap:${opus} `) ? [l, `a=fmtp:${opus} usedtx=1`] : [l]))
}

/**
 * Asks, in the SFU's offer, for discontinuous transmission on the microphone
 * line (mid): the browser then sends only about 5 small packets a second while
 * the encoder finds nothing but silence, instead of 50 (RFC 7587 usedtx).
 * It decides by the signal: true silence and quiet room noise count as
 * silence, speech never does, and a steady or very quiet musical sound may.
 * The SFU forwards the packets as they come, with their timestamp gaps, and
 * the listeners' browsers fill the gaps with comfort noise.
 * The screen's sound is left alone: music and game sound have quiet passages
 * that are not silence.
 */
export function enableMicDtx(sdp: string, mid: string | null | undefined) {
  if (!sdp || mid === null || mid === undefined) return sdp
  const eol = sdp.includes('\r\n') ? '\r\n' : '\n'
  const lines = sdp.split(/\r?\n/)
  for (const [from, to] of sectionBounds(lines)) {
    const section = lines.slice(from, to)
    if (!section[0]?.startsWith('m=audio') || !section.includes(`a=mid:${mid}`)) continue
    lines.splice(from, to - from, ...dtxSection(section))
    break
  }
  return lines.join(eol)
}
