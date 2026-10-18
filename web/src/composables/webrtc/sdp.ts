/**
 * The SFU creates our four receive lines before anything else, so they are
 * the offer's first two audio lines (microphone, screen share's sound) and
 * first two video lines (screen, camera). Their direction says nothing: the
 * SFU may reuse them to forward other members' media (then they read
 * sendrecv/sendonly).
 */
export function publishMids(sdp: string | null | undefined, layerLines = 0) {
  const out: { audio: string | null; screenAudio: string | null; video: string[] } = { audio: null, screenAudio: null, video: [] }
  let kind: string | undefined | null = null
  let mid: string | null = null
  const flush = () => {
    if (!kind || mid === null) return
    if (kind === 'audio' && out.audio === null) out.audio = mid
    else if (kind === 'audio' && out.screenAudio === null) out.screenAudio = mid
    else if (kind === 'video' && out.video.length < 2 + layerLines) out.video.push(mid)
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
  return out
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
  const starts: number[] = []
  lines.forEach((l, i) => { if (l.startsWith('m=')) starts.push(i) })
  for (let n = starts.length - 1; n >= 0; n--) {
    const from = starts[n]
    if (from === undefined) continue
    const to = starts[n + 1] ?? lines.length
    const section = lines.slice(from, to)
    if (!section[0]?.startsWith('m=video')) continue
    const names = new Map<string, string>()
    const apt = new Map<string, string>()
    for (const l of section) {
      let m = /^a=rtpmap:(\d+) ([^/]+)\//.exec(l)
      if (m?.[1] && m[2]) names.set(m[1], m[2].toLowerCase())
      m = /^a=fmtp:(\d+) .*\bapt=(\d+)/.exec(l)
      if (m?.[1] && m[2]) apt.set(m[1], m[2])
    }
    const keep = new Set<string>()
    for (const [pt, name] of names) if (SEALABLE_VIDEO.has(name)) keep.add(pt)
    for (const [pt, name] of names) if (name === 'rtx' && keep.has(apt.get(pt) || '')) keep.add(pt)
    const listed = section[0].split(' ').slice(3)
    if (!listed.some(pt => SEALABLE_VIDEO.has(names.get(pt) || ''))) continue
    const out = section.flatMap((l) => {
      if (l.startsWith('m=')) return [[...l.split(' ').slice(0, 3), ...listed.filter(pt => keep.has(pt))].join(' ')]
      const m = /^a=(?:rtpmap|fmtp|rtcp-fb):(\d+)\b/.exec(l)
      return m?.[1] && !keep.has(m[1]) ? [] : [l]
    })
    lines.splice(from, to - from, ...out)
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
  // Section boundaries: [start, end) per m= line.
  const starts: number[] = []
  lines.forEach((l, i) => { if (l.startsWith('m=')) starts.push(i) })
  for (let n = 0; n < starts.length; n++) {
    const from = starts[n]
    if (from === undefined) continue
    const to = starts[n + 1] ?? lines.length
    const section = lines.slice(from, to)
    if (!section[0]?.startsWith('m=video') || !section.includes(`a=mid:${mid}`)) continue

    const names = new Map<string, string>()
    const fmtps = new Map<string, string>()
    for (const l of section) {
      let m = /^a=rtpmap:(\d+) ([^/]+)\//.exec(l)
      if (m?.[1] && m[2]) names.set(m[1], m[2].toLowerCase())
      m = /^a=fmtp:(\d+) (.*)$/.exec(l)
      if (m?.[1] && m[2] !== undefined) fmtps.set(m[1], m[2])
    }
    const media = (pt: string) => names.has(pt) && !AUX_CODECS.has(names.get(pt) || '')

    const out = []
    for (const l of section) {
      if (l.startsWith('m=') && h264) {
        const parts = l.split(' ')
        const pts = parts.slice(3)
        const rank = (pt: string) => (media(pt) ? h264Rank(names.get(pt) || '', fmtps.get(pt) || '') : 5)
        const sorted = pts.map((pt, i): [string, number] => [pt, i]).sort((a, b) => rank(a[0]) - rank(b[0]) || a[1] - b[1]).map(e => e[0])
        out.push([...parts.slice(0, 3), ...sorted].join(' '))
        continue
      }
      let m = startKbps ? /^a=fmtp:(\d+) (.*)$/.exec(l) : null
      if (m?.[1] && m[2] !== undefined && media(m[1]) && !m[2].includes('x-google-')) {
        out.push(`${l};x-google-start-bitrate=${startKbps};x-google-min-bitrate=${minKbps}`)
        continue
      }
      out.push(l)
      m = startKbps ? /^a=rtpmap:(\d+) /.exec(l) : null
      if (m?.[1] && media(m[1]) && !fmtps.has(m[1])) {
        out.push(`a=fmtp:${m[1]} x-google-start-bitrate=${startKbps};x-google-min-bitrate=${minKbps}`)
      }
    }
    lines.splice(from, to - from, ...out)
    break
  }
  return lines.join(eol)
}

/** tuneScreenOffer for the screen line and the lines of its lower layers. */
export function tuneScreenLines(sdp: string, mids: { video: string[] }, h264: boolean) {
  let tuned = sdp
  for (const mid of [mids.video[0], ...mids.video.slice(2)]) tuned = tuneScreenOffer(tuned, mid, { h264 })
  return tuned
}
