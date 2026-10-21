// A browser packetizes and depacketizes video from the frame bytes it is
// handed, so a sealed frame must still look like a frame of its codec: H.264
// is cut at Annex B start codes and read for NAL types, slice headers and
// parameter sets, VP8 is classified by its first payload byte. Everything the
// network path parses stays readable (and authenticated); the rest is sealed.
// Audio and codecs that are parsed from the RTP descriptor alone are sealed
// whole.

export type FrameKind = 'key' | 'delta' | undefined

/** Video codecs this module knows how to seal. Anything else is refused. */
export type VideoCodec = 'h264' | 'vp8'

/** The codec of an encoded video frame, from its metadata mime type. */
export function videoCodecOf(mimeType: unknown): VideoCodec | null {
  const name = typeof mimeType === 'string' ? mimeType.toLowerCase() : ''
  if (name === 'video/h264') return 'h264'
  if (name === 'video/vp8') return 'vp8'
  return null
}

/** Seals the bytes after `clear`. `clear` is bound as authenticated data. */
export type SealBytes = (plaintext: Uint8Array, clear: Uint8Array) => Promise<Uint8Array | null>
/** Opens what SealBytes produced for the same `clear`. */
export type OpenBytes = (packet: Uint8Array, clear: Uint8Array) => Promise<Uint8Array | null>

// VP8: the payload header is 10 bytes on a key frame (frame tag, start code,
// dimensions) and 3 on a delta frame.
const VP8_KEY_CLEAR = 10
const VP8_DELTA_CLEAR = 3

// H.264: NAL types 1 (slice) and 5 (IDR slice) carry picture data. The NAL
// header and the slice header up to and including pps_id stay readable,
// because the depacketizer looks up the parameter set from them.
//
// A frame may only hold these NAL types. Parameter sets (7, 8) and the access
// unit delimiter (9) stay readable, so every readable byte of a frame is bound
// into the authenticated data of each picture NAL. SEI (6) is removed before
// sealing and refused on receipt, and so is anything else.
const NAL_SLICE = 1
const NAL_IDR = 5
const NAL_SEI = 6
const NAL_PARAMETER_SETS = new Set([7, 8])
const NAL_AUD = 9
// first_mb_in_slice, slice_type and pps_id are ue(v): at most 32 + 32 + 32
// bits, which a real stream never approaches (4K has about 32k macroblocks).
const SLICE_HEADER_MAX_BYTES = 12
const UE_MAX_ZEROS = 32
// A sealed NAL ends in a byte that is never zero, so no trailing zero merges
// with the next start code.
const NAL_TRAILER = 0x80
// Keeps one framing's sealed bytes from opening as another's.
const TAG_H264 = 0x48
const TAG_VP8 = 0x56

function startsWithStartCode(data: Uint8Array): boolean {
  return data.length > 3 && data[0] === 0 && data[1] === 0 && (data[2] === 1 || (data[2] === 0 && data[3] === 1))
}

interface Nal {
  /** First byte of the NAL header. */
  start: number
  /** One past the last byte of the NAL, before the next start code's zeros. */
  end: number
}

function isStartCode(data: Uint8Array, i: number): boolean {
  return data[i] === 0 && data[i + 1] === 0 && data[i + 2] === 1
}

function trimTrailingZeros(data: Uint8Array, start: number, end: number): number {
  let at = end
  while (at > start && data[at - 1] === 0) at--
  return at
}

function splitNals(data: Uint8Array): Nal[] {
  const nals: Nal[] = []
  let open = -1
  for (let i = 0; i + 2 < data.length; i++) {
    if (!isStartCode(data, i)) continue
    if (open >= 0) {
      nals.push({ start: open, end: trimTrailingZeros(data, open, i) })
    }
    open = i + 3
    i += 2
  }
  if (open >= 0) nals.push({ start: open, end: data.length })
  return nals
}

function nalType(header: number): number {
  return header & 0x1f
}

function isPictureType(type: number): boolean {
  return type === NAL_SLICE || type === NAL_IDR
}

/**
 * Bytes from the start of a slice NAL (header included) through the byte that
 * holds the end of pps_id, counting emulation prevention bytes. Null when the
 * header does not fit.
 */
function sliceHeaderBytes(nal: Uint8Array): number | null {
  const { bits, byteOf } = unescapedBits(nal)
  let at = 0
  for (let value = 0; value < 3; value++) {
    const next = skipExpGolomb(bits, at)
    if (next === null) return null
    at = next
  }
  const last = byteOf[at - 1]
  return last === undefined ? null : last + 1
}

/** The slice header bits after the NAL header, with their source byte index. */
function unescapedBits(nal: Uint8Array): { bits: number[]; byteOf: number[] } {
  const bits: number[] = []
  const byteOf: number[] = []
  let zeros = 0
  for (let i = 1; i < nal.length && i <= SLICE_HEADER_MAX_BYTES; i++) {
    const byte = nal[i] as number
    if (zeros >= 2 && byte === 3) {
      zeros = 0
      continue
    }
    zeros = byte === 0 ? zeros + 1 : 0
    for (let bit = 7; bit >= 0; bit--) {
      bits.push((byte >> bit) & 1)
      byteOf.push(i)
    }
  }
  return { bits, byteOf }
}

/** Position after one ue(v) code starting at `at`, or null when it does not fit. */
function skipExpGolomb(bits: number[], at: number): number | null {
  let leading = 0
  while (bits[at] === 0) {
    leading++
    at++
    if (leading > UE_MAX_ZEROS) return null
  }
  at += 1 + leading
  return at > bits.length ? null : at
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

interface Unit {
  /** The start code in front of the NAL: zeros and a one. */
  gap: Uint8Array
  nal: Uint8Array
  picture: boolean
  /** Bytes that stay readable. */
  readable: number
}

type ParsedUnit =
  | { kind: 'unit'; unit: Unit }
  | { kind: 'drop' }
  | { kind: 'invalid' }

function parsePictureUnit(gap: Uint8Array, nal: Uint8Array): ParsedUnit {
  const readable = sliceHeaderBytes(nal)
  if (readable === null || nal.length <= readable) return { kind: 'invalid' }
  return { kind: 'unit', unit: { gap, nal, picture: true, readable } }
}

function parseNonPictureUnit(gap: Uint8Array, nal: Uint8Array, type: number): ParsedUnit {
  if (NAL_PARAMETER_SETS.has(type) || type === NAL_AUD) {
    return { kind: 'unit', unit: { gap, nal, picture: false, readable: nal.length } }
  }
  return { kind: 'invalid' }
}

function parseNalUnit(gap: Uint8Array, nal: Uint8Array, dropSei: boolean): ParsedUnit {
  const header = nal[0]
  if (header === undefined || (header & 0x80) !== 0) return { kind: 'invalid' }
  const type = nalType(header)
  if (type === NAL_SEI && dropSei) return { kind: 'drop' }
  if (isPictureType(type)) return parsePictureUnit(gap, nal)
  return parseNonPictureUnit(gap, nal, type)
}

/**
 * Cuts an Annex B frame into units and checks it. Null when it is not Annex B,
 * has a NAL type that is not allowed, or has no picture NAL. SEI is removed
 * when `dropSei` is set and refused otherwise.
 */
function parseFrame(data: Uint8Array, dropSei: boolean): Unit[] | null {
  if (!startsWithStartCode(data)) return null
  const units: Unit[] = []
  let at = 0
  let pictures = 0

  for (const found of splitNals(data)) {
    const gap = data.subarray(at, found.start)
    at = found.end
    const nal = data.subarray(found.start, found.end)
    const parsed = parseNalUnit(gap, nal, dropSei)
    if (parsed.kind === 'invalid') return null
    if (parsed.kind === 'drop') continue
    if (parsed.unit.picture) pictures++
    units.push(parsed.unit)
  }

  return pictures > 0 ? units : null
}

function word(value: number): Uint8Array {
  const out = new Uint8Array(4)
  new DataView(out.buffer).setUint32(0, value)
  return out
}

/** Every readable byte of the frame in order, each NAL with its length. */
function frameContext(units: readonly Unit[]): Uint8Array {
  const parts: Uint8Array[] = [Uint8Array.of(TAG_H264), word(units.length)]
  for (const unit of units) parts.push(word(unit.readable), unit.nal.subarray(0, unit.readable))
  return join(parts)
}

async function sealH264(data: Uint8Array, seal: SealBytes): Promise<Uint8Array | null> {
  const units = parseFrame(data, true)
  if (!units) return null
  const context = frameContext(units)
  const parts: Uint8Array[] = []
  for (const [index, unit] of units.entries()) {
    parts.push(unit.gap)
    if (!unit.picture) {
      parts.push(unit.nal)
      continue
    }
    const clear = unit.nal.subarray(0, unit.readable)
    const sealed = await seal(unit.nal.subarray(unit.readable), join([context, word(index)]))
    if (!sealed) return null
    parts.push(clear, escapeNal(sealed, trailingZeros(clear)), Uint8Array.of(NAL_TRAILER))
  }
  return join(parts)
}

async function openH264(data: Uint8Array, open: OpenBytes): Promise<Uint8Array | null> {
  const units = parseFrame(data, false)
  if (!units) return null
  const context = frameContext(units)
  const parts: Uint8Array[] = []
  for (const [index, unit] of units.entries()) {
    parts.push(unit.gap)
    if (!unit.picture) {
      parts.push(unit.nal)
      continue
    }
    const last = unit.nal.length - 1
    if (last <= unit.readable || unit.nal[last] !== NAL_TRAILER) return null
    const clear = unit.nal.subarray(0, unit.readable)
    const sealed = unescapeNal(unit.nal.subarray(unit.readable, last), trailingZeros(clear))
    const opened = await open(sealed, join([context, word(index)]))
    if (!opened) return null
    parts.push(clear, opened)
  }
  return join(parts)
}

function vp8Context(kind: FrameKind, clear: Uint8Array): Uint8Array {
  return join([Uint8Array.of(TAG_VP8, kind === 'key' ? 1 : 0), clear])
}

function vp8ClearBytes(kind: FrameKind): number | null {
  if (kind === 'key') return VP8_KEY_CLEAR
  if (kind === 'delta') return VP8_DELTA_CLEAR
  return null
}

/**
 * Seals one encoded frame. Audio has no kind and no codec and is sealed whole.
 * A video frame needs a known codec; an unknown one is refused, never guessed.
 */
export async function sealMedia(data: Uint8Array, kind: FrameKind, codec: VideoCodec | null, seal: SealBytes): Promise<Uint8Array | null> {
  if (kind === undefined) return seal(data, new Uint8Array())
  if (codec === 'h264') return sealH264(data, seal)
  if (codec !== 'vp8') return null
  const clear = vp8ClearBytes(kind)
  if (clear === null || data.length < clear) return null
  const sealed = await seal(data.subarray(clear), vp8Context(kind, data.subarray(0, clear)))
  return sealed ? join([data.subarray(0, clear), sealed]) : null
}

export async function openMedia(data: Uint8Array, kind: FrameKind, codec: VideoCodec | null, open: OpenBytes): Promise<Uint8Array | null> {
  if (kind === undefined) return open(data, new Uint8Array())
  if (codec === 'h264') return openH264(data, open)
  if (codec !== 'vp8') return null
  const clear = vp8ClearBytes(kind)
  if (clear === null || data.length < clear) return null
  const opened = await open(data.subarray(clear), vp8Context(kind, data.subarray(0, clear)))
  return opened ? join([data.subarray(0, clear), opened]) : null
}
