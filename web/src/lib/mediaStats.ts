/** Browser WebRTC dictionaries vary by implementation; missing fields stay absent. */
export interface MediaStat {
  id: string
  type: string
  timestamp?: number
  kind?: string
  mediaType?: string
  mimeType?: string
  codecId?: string
  clockRate?: number
  channels?: number
  nominated?: boolean
  selected?: boolean
  state?: string
  selectedCandidatePairId?: string
  localCandidateId?: string
  remoteCandidateId?: string
  candidateType?: string
  protocol?: string
  dtlsCipher?: string
  srtpCipher?: string
  currentRoundTripTime?: number
  packetsReceived?: number
  packetsLost?: number
  packetsSent?: number
  jitter?: number
  bytesReceived?: number
  bytesSent?: number
  frameWidth?: number
  frameHeight?: number
  framesPerSecond?: number
  framesEncoded?: number
  totalEncodeTime?: number
  mediaSourceId?: string
  encoderImplementation?: string
  decoderImplementation?: string
  powerEfficientEncoder?: boolean
  powerEfficientDecoder?: boolean
  qualityLimitationReason?: string
  sdpFmtpLine?: string
  framesDropped?: number
  freezeCount?: number
  nackCount?: number
  pliCount?: number
}

export function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}
