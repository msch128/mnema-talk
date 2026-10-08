//! Canonical transport attribution carried INSIDE a protected source envelope.
//! A producer claim validates shape only. Even an authenticated binding still
//! needs native current Voice membership, offer/MID, and source-lease checks;
//! this module neither issues an RTC grant nor authenticates SFU metadata.
use super::{Error, Result, SourceKind};
use coset::cbor::value::Value;

pub const MAX_CAPTURE_GENERATION: u64 = (1u64 << 53) - 1;
pub const MAX_PUBLISHER_TRACK_ID: usize = 256;

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum SourcePurpose {
    Voice,
    Camera,
    Screen,
}
impl SourcePurpose {
    pub fn name(self) -> &'static str {
        match self {
            Self::Voice => "voice",
            Self::Camera => "camera",
            Self::Screen => "screen",
        }
    }
    fn checked_codec(self, codec: SourceKind) -> Result<()> {
        match (self, codec) {
            (Self::Voice, SourceKind::Opus)
            | (Self::Camera, SourceKind::Vp8)
            | (Self::Screen, SourceKind::Opus | SourceKind::Vp8) => Ok(()),
            _ => Err(Error::Invalid),
        }
    }
    fn parse(value: &Value) -> Result<Self> {
        match value {
            Value::Text(name) if name == "voice" => Ok(Self::Voice),
            Value::Text(name) if name == "camera" => Ok(Self::Camera),
            Value::Text(name) if name == "screen" => Ok(Self::Screen),
            _ => Err(Error::Invalid),
        }
    }
}

/// Native producer CLAIM, not an authenticated source or a media capability.
/// Track ID is the original publisher track ID, without an SFU account prefix.
/// Codec comes from the containing source envelope, not the six-field tail.
pub struct SourceTransportClaim {
    codec: SourceKind,
    voice_channel: String,
    room_incarnation: String,
    publisher_connection: String,
    purpose: SourcePurpose,
    capture_generation: u64,
    publisher_track_id: String,
}
impl SourceTransportClaim {
    pub fn claim(
        codec: SourceKind,
        voice_channel: &str,
        room_incarnation: &str,
        publisher_connection: &str,
        purpose: SourcePurpose,
        capture_generation: u64,
        publisher_track_id: &str,
    ) -> Result<Self> {
        for value in [voice_channel, room_incarnation, publisher_connection] {
            super::uuid(value)?;
        }
        purpose.checked_codec(codec)?;
        if capture_generation == 0
            || capture_generation > MAX_CAPTURE_GENERATION
            || publisher_track_id.is_empty()
            || publisher_track_id.len() > MAX_PUBLISHER_TRACK_ID
            || !publisher_track_id
                .bytes()
                .all(|byte| (0x21..=0x7e).contains(&byte))
        {
            return Err(Error::Invalid);
        }
        Ok(Self {
            codec,
            voice_channel: voice_channel.into(),
            room_incarnation: room_incarnation.into(),
            publisher_connection: publisher_connection.into(),
            purpose,
            capture_generation,
            publisher_track_id: publisher_track_id.into(),
        })
    }
    pub fn codec(&self) -> SourceKind {
        self.codec
    }
    pub fn voice_channel(&self) -> &str {
        &self.voice_channel
    }
    pub fn room_incarnation(&self) -> &str {
        &self.room_incarnation
    }
    pub fn publisher_connection(&self) -> &str {
        &self.publisher_connection
    }
    pub fn purpose(&self) -> SourcePurpose {
        self.purpose
    }
    pub fn capture_generation(&self) -> u64 {
        self.capture_generation
    }
    pub fn publisher_track_id(&self) -> &str {
        &self.publisher_track_id
    }

    /// Only the native source-envelope encoder uses this canonical flat tail.
    pub(crate) fn payload_tail(&self) -> Vec<Value> {
        vec![
            Value::Text(self.voice_channel.clone()),
            Value::Text(self.room_incarnation.clone()),
            Value::Text(self.publisher_connection.clone()),
            Value::Text(self.purpose.name().into()),
            Value::Integer(self.capture_generation.into()),
            Value::Text(self.publisher_track_id.clone()),
        ]
    }
}

/// Authenticated attribution ONLY when the SDK dispatcher constructs it after
/// one actual MLS decrypt and exact protected-envelope/sender/domain checks.
/// No Serde, public constructor, source/event/account/device substitution, or
/// raw-source-lease getter. Native owner must still verify current Voice facts.
pub struct ProtectedSourceBinding {
    claim: SourceTransportClaim,
}
impl ProtectedSourceBinding {
    pub(crate) fn from_authenticated_fields(codec: SourceKind, fields: &[Value]) -> Result<Self> {
        let [
            Value::Text(channel),
            Value::Text(room),
            Value::Text(connection),
            purpose,
            Value::Integer(generation),
            Value::Text(track),
        ] = fields
        else {
            return Err(Error::Invalid);
        };
        let capture_generation = u64::try_from(*generation).map_err(|_| Error::Invalid)?;
        let claim = SourceTransportClaim::claim(
            codec,
            channel,
            room,
            connection,
            SourcePurpose::parse(purpose)?,
            capture_generation,
            track,
        )?;
        Ok(Self { claim })
    }
    pub fn codec(&self) -> SourceKind {
        self.claim.codec()
    }
    pub fn voice_channel(&self) -> &str {
        self.claim.voice_channel()
    }
    pub fn room_incarnation(&self) -> &str {
        self.claim.room_incarnation()
    }
    pub fn publisher_connection(&self) -> &str {
        self.claim.publisher_connection()
    }
    pub fn purpose(&self) -> SourcePurpose {
        self.claim.purpose()
    }
    pub fn capture_generation(&self) -> u64 {
        self.claim.capture_generation()
    }
    pub fn publisher_track_id(&self) -> &str {
        self.claim.publisher_track_id()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    const CHANNEL: &str = "11111111-1111-4111-8111-111111111111";
    const ROOM: &str = "22222222-2222-4222-8222-222222222222";
    const CONNECTION: &str = "33333333-3333-4333-8333-333333333333";

    fn claim(
        codec: SourceKind,
        purpose: SourcePurpose,
        generation: u64,
        track: &str,
    ) -> Result<SourceTransportClaim> {
        SourceTransportClaim::claim(codec, CHANNEL, ROOM, CONNECTION, purpose, generation, track)
    }
    fn tail() -> Vec<Value> {
        claim(
            SourceKind::Opus,
            SourcePurpose::Voice,
            7,
            "microphone-track",
        )
        .unwrap()
        .payload_tail()
    }
    fn rejected(fields: &[Value]) {
        assert!(matches!(
            ProtectedSourceBinding::from_authenticated_fields(SourceKind::Opus, fields),
            Err(Error::Invalid)
        ));
    }

    #[test]
    fn all_supported_purpose_codec_pairs_roundtrip_exact_attribution() {
        for (codec, purpose) in [
            (SourceKind::Opus, SourcePurpose::Voice),
            (SourceKind::Vp8, SourcePurpose::Camera),
            (SourceKind::Opus, SourcePurpose::Screen),
            (SourceKind::Vp8, SourcePurpose::Screen),
        ] {
            let input = claim(codec, purpose, MAX_CAPTURE_GENERATION, "track-:!~").unwrap();
            let fields = input.payload_tail();
            let mut wire = Vec::new();
            coset::cbor::ser::into_writer(&Value::Array(fields.clone()), &mut wire).unwrap();
            let Value::Array(decoded) = coset::cbor::de::from_reader(wire.as_slice()).unwrap()
            else {
                panic!("encoded tail must be an array");
            };
            assert_eq!(decoded, fields);
            let binding =
                ProtectedSourceBinding::from_authenticated_fields(codec, &decoded).unwrap();
            assert_eq!(binding.codec(), codec);
            assert_eq!(binding.voice_channel(), CHANNEL);
            assert_eq!(binding.room_incarnation(), ROOM);
            assert_eq!(binding.publisher_connection(), CONNECTION);
            assert!(binding.purpose() == purpose);
            assert_eq!(binding.capture_generation(), MAX_CAPTURE_GENERATION);
            assert_eq!(binding.publisher_track_id(), "track-:!~");
        }
    }
    #[test]
    fn voice_video_and_camera_audio_are_rejected_before_encoding_and_after_receipt() {
        for (codec, purpose) in [
            (SourceKind::Vp8, SourcePurpose::Voice),
            (SourceKind::Opus, SourcePurpose::Camera),
        ] {
            assert!(claim(codec, purpose, 1, "track").is_err());
            let mut fields = tail();
            fields[3] = Value::Text(purpose.name().into());
            assert!(ProtectedSourceBinding::from_authenticated_fields(codec, &fields).is_err());
        }
    }
    #[test]
    fn canonical_non_nil_uuid_required_for_each_transport_identity() {
        for invalid in [
            "",
            "00000000-0000-0000-0000-000000000000",
            "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA",
            "11111111111141118111111111111111",
            "11111111-1111-4111-8111-11111111111g",
            "11111111-1111-4111-8111-111111111111 ",
        ] {
            for index in 0..3 {
                let mut fields = tail();
                fields[index] = Value::Text(invalid.into());
                rejected(&fields);
            }
        }
    }
    #[test]
    fn capture_generation_is_positive_integer_with_exact_js_comparator_range() {
        for generation in [0, MAX_CAPTURE_GENERATION + 1, u64::MAX] {
            assert!(claim(SourceKind::Opus, SourcePurpose::Voice, generation, "track").is_err());
            let mut fields = tail();
            fields[4] = Value::Integer(generation.into());
            rejected(&fields);
        }
        for invalid in [
            Value::Integer((-1).into()),
            Value::Float(7.0),
            Value::Text("7".into()),
        ] {
            let mut fields = tail();
            fields[4] = invalid;
            rejected(&fields);
        }
        assert!(claim(SourceKind::Opus, SourcePurpose::Voice, 1, "track").is_ok());
    }
    #[test]
    fn original_track_accepts_visible_ascii_boundaries_without_normalization() {
        for track in ["!".into(), "~".into(), "x".repeat(MAX_PUBLISHER_TRACK_ID)] {
            let input = claim(SourceKind::Opus, SourcePurpose::Voice, 1, &track).unwrap();
            let binding = ProtectedSourceBinding::from_authenticated_fields(
                SourceKind::Opus,
                &input.payload_tail(),
            )
            .unwrap();
            assert_eq!(binding.publisher_track_id(), track);
        }
        for track in [
            "".into(),
            "x".repeat(MAX_PUBLISHER_TRACK_ID + 1),
            " track".into(),
            "track ".into(),
            "track\t".into(),
            "track\n".into(),
            "track\0".into(),
            "track\u{7f}".into(),
            "träck".into(),
        ] {
            let mut fields = tail();
            fields[5] = Value::Text(track);
            rejected(&fields);
        }
    }
    #[test]
    fn exact_tail_arity_types_and_closed_purpose_names_required() {
        let good = tail();
        for length in 0..6 {
            rejected(&good[..length]);
        }
        let mut extra = good.clone();
        extra.push(Value::Null);
        rejected(&extra);
        for index in 0..6 {
            for invalid in [
                Value::Null,
                Value::Bool(true),
                Value::Bytes(vec![1]),
                Value::Array(vec![]),
                Value::Map(vec![]),
            ] {
                let mut fields = good.clone();
                fields[index] = invalid;
                rejected(&fields);
            }
        }
        for name in ["", "Voice", "screen_audio", "camera ", "voice\0", "unknown"] {
            let mut fields = good.clone();
            fields[3] = Value::Text(name.into());
            rejected(&fields);
        }
    }
}
