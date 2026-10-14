// A browser packetizes and depacketizes video from the frame bytes it is
// handed, so a sealed frame must still look like a frame of its codec: H.264
// is cut at Annex B start codes and read for NAL types, slice headers and
// parameter sets, VP8 is classified by its first payload byte. Everything the
// network path parses stays readable (and authenticated); the rest is sealed.
// Audio and codecs that are parsed from the RTP descriptor alone are sealed
// whole.

export type FrameKind = 'key' | 'delta' | undefined

/** Seals the bytes after `clear`. `clear` is bound as authenticated data. */
export type SealBytes = (plaintext: Uint8Array, clear: Uint8Array) => Promise<Uint8Array | null>
/** Opens what SealBytes produced for the same `clear`. */
export type OpenBytes = (packet: Uint8Array, clear: Uint8Array) => Promise<Uint8Array | null>

// VP8: the payload header is 10 bytes on a key frame (frame tag, start code,
// dimensions) and 3 on a delta frame.
const VP8_KEY_CLEAR = 10
const VP8_DELTA_CLEAR = 3

// H.264: NAL types 1 (slice) and 5 (IDR slice) carry picture data. The NAL
// header and the start of the slice header (first MB, slice type, PPS id) stay
// readable because the depacketizer looks up the parameter set from them.
const NAL_SLICE = 1
const NAL_IDR = 5
const NAL_HEADER_BYTES = 1
const SLICE_CLEAR_BYTES = 3
// A sealed NAL ends in a byte that is never zero, so no trailing zero merges
// with the next start code.
const NAL_TRAILER = 0x80

function isAnnexB(data: Uint8Array): boolean {
  return (data.length > 3 && data[0] === 0 && data[1] === 0 && (data[2] === 1 || (data[2] === 0 && data[3] === 1)))
}

interface Nal {
  /** First byte of the NAL header. */
  start: number
  /** One past the last byte of the NAL, before the next start code's zeros. */
  end: number
}

function splitNals(data: Uint8Array): Nal[] {
  const nals: Nal[] = []
  let open = -1
  for (let i = 0; i + 2 < data.length; i++) {
    if (data[i] !== 0 || data[i + 1] !== 0 || data[i + 2] !== 1) continue
    if (open >= 0) {
      let end = i
      while (end > open && data[end - 1] === 0) end--
      nals.push({ start: open, end })
    }
    open = i + 3
    i += 2
  }
  if (open >= 0) nals.push({ start: open, end: data.length })
  return nals
}

function isPicture(header: number): boolean {
  const type = header & 0x1f
  return type === NAL_SLICE || type === NAL_IDR
}

/** Inserts 0x03 wherever two zeros would be followed by 0x00..0x03. */
function escapeNal(bytes: Uint8Array, zeros: number): Uint8Array {
  const out = new Uint8Array(bytes.length + (bytes.length >> 1) + 1)
  let at = 0
  for (const byte of bytes) {
    if (zeros >= 2 && byte <= 3) {
      out[at++] = 3
      zeros = 0
    }
    out[at++] = byte
    zeros = byte === 0 ? zeros + 1 : 0
  }
  return out.subarray(0, at)
}

function unescapeNal(bytes: Uint8Array, zeros: number): Uint8Array {
  const out = new Uint8Array(bytes.length)
  let at = 0
  for (const byte of bytes) {
    if (zeros >= 2 && byte === 3) {
      zeros = 0
      continue
    }
    out[at++] = byte
    zeros = byte === 0 ? zeros + 1 : 0
  }
  return out.subarray(0, at)
}

function trailingZeros(bytes: Uint8Array): number {
  let zeros = 0
  for (let i = bytes.length - 1; i >= 0 && bytes[i] === 0 && zeros < 2; i--) zeros++
  return zeros
}

function join(parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((sum, part) => sum + part.length, 0))
  let at = 0
  for (const part of parts) {
    out.set(part, at)
    at += part.length
  }
  return out
}

async function mapNals(data: Uint8Array, one: (nal: Uint8Array) => Promise<Uint8Array | null>): Promise<Uint8Array | null> {
  const parts: Uint8Array[] = []
  let at = 0
  for (const nal of splitNals(data)) {
    parts.push(data.subarray(at, nal.start))
    const mapped = await one(data.subarray(nal.start, nal.end))
    if (!mapped) return null
    parts.push(mapped)
    at = nal.end
  }
  parts.push(data.subarray(at))
  return join(parts)
}

async function sealNal(nal: Uint8Array, seal: SealBytes): Promise<Uint8Array | null> {
  const header = nal[0]
  if (header === undefined || !isPicture(header)) return nal
  const clear = nal.subarray(0, NAL_HEADER_BYTES + SLICE_CLEAR_BYTES)
  const rest = nal.subarray(clear.length)
  if (rest.length === 0) return null
  const sealed = await seal(rest, clear)
  if (!sealed) return null
  return join([clear, escapeNal(sealed, trailingZeros(clear)), Uint8Array.of(NAL_TRAILER)])
}

async function openNal(nal: Uint8Array, open: OpenBytes): Promise<Uint8Array | null> {
  const header = nal[0]
  if (header === undefined || !isPicture(header)) return nal
  const clearLength = NAL_HEADER_BYTES + SLICE_CLEAR_BYTES
  if (nal.length <= clearLength + 1 || nal[nal.length - 1] !== NAL_TRAILER) return null
  const clear = nal.subarray(0, clearLength)
  const sealed = unescapeNal(nal.subarray(clearLength, nal.length - 1), trailingZeros(clear))
  const opened = await open(sealed, clear)
  return opened ? join([clear, opened]) : null
}

function clearBytes(kind: FrameKind): number {
  if (kind === 'key') return VP8_KEY_CLEAR
  if (kind === 'delta') return VP8_DELTA_CLEAR
  return 0
}

export async function sealMedia(data: Uint8Array, kind: FrameKind, seal: SealBytes): Promise<Uint8Array | null> {
  if (kind !== undefined && isAnnexB(data)) return mapNals(data, nal => sealNal(nal, seal))
  const clear = clearBytes(kind)
  if (data.length < clear) return null
  const sealed = await seal(data.subarray(clear), data.subarray(0, clear))
  return sealed ? join([data.subarray(0, clear), sealed]) : null
}

export async function openMedia(data: Uint8Array, kind: FrameKind, open: OpenBytes): Promise<Uint8Array | null> {
  if (kind !== undefined && isAnnexB(data)) return mapNals(data, nal => openNal(nal, open))
  const clear = clearBytes(kind)
  if (data.length < clear) return null
  const opened = await open(data.subarray(clear), data.subarray(0, clear))
  return opened ? join([data.subarray(0, clear), opened]) : null
}
