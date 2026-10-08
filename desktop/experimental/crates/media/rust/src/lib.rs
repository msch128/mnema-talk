//! Ignored native-only synthetic fixture policy, never a production MLS grant.
//! Broker provenance and real native-window/TLS/lease checks remain mandatory.
use serde::{Deserialize, Serialize};
use std::{
    fmt,
    path::Path,
    sync::atomic::{AtomicBool, Ordering},
    time::Instant,
};
use uuid::Uuid;
use zeroize::Zeroizing;

pub const DUMMY_PASSWORD: &str = "native-fixture-password";
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    Denied,
    Protocol,
    BodyLimit,
    QualificationRequired,
    Expired,
}
impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("synthetic native media policy rejected")
    }
}
impl std::error::Error for Error {}
#[derive(Clone, Copy, PartialEq, Eq)]
pub struct Id(Uuid);
impl Id {
    pub fn native(value: Uuid) -> Result<Self, Error> {
        if value.is_nil() {
            Err(Error::Protocol)
        } else {
            Ok(Self(value))
        }
    }
    pub fn uuid(self) -> Uuid {
        self.0
    }
}
impl Serialize for Id {
    fn serialize<S: serde::Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(&self.0.hyphenated().to_string())
    }
}
impl fmt::Debug for Id {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeFixtureId(REDACTED)")
    }
}
impl<'de> Deserialize<'de> for Id {
    fn deserialize<D: serde::Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        let v = String::deserialize(d)?;
        let id = Uuid::parse_str(&v)
            .map_err(|_| serde::de::Error::custom("invalid fixture identity"))?;
        if id.is_nil() || id.hyphenated().to_string() != v {
            return Err(serde::de::Error::custom("invalid fixture identity"));
        }
        Ok(Self(id))
    }
}
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Role {
    Publisher,
    Viewer,
}
#[derive(Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum AnswerKind {
    Answer,
}
#[derive(Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum OfferKind {
    Offer,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Answer {
    #[serde(rename = "type")]
    pub kind: AnswerKind,
    pub sdp: String,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Offer {
    #[serde(rename = "type")]
    pub kind: OfferKind,
    pub sdp: String,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Candidate {
    pub candidate: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub sdp_mid: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub sdp_m_line_index: Option<u16>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub username_fragment: Option<String>,
}
impl Candidate {
    pub fn validate(&self) -> Result<(), Error> {
        let text = |s: &str, n| s.len() <= n && !s.chars().any(char::is_control);
        if !text(&self.candidate, 4096)
            || self.sdp_mid.as_ref().is_some_and(|s| !text(s, 64))
            || self.sdp_m_line_index.is_some_and(|n| n > 32)
            || self
                .username_fragment
                .as_ref()
                .is_some_and(|s| !text(s, 256))
        {
            Err(Error::Protocol)
        } else {
            Ok(())
        }
    }
}
#[derive(Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SubscriptionKind {
    Screen,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(tag = "type", content = "payload", deny_unknown_fields)]
pub enum Action {
    #[serde(rename = "voice_join")]
    Join { channel_id: Id },
    #[serde(rename = "voice_leave")]
    Leave,
    #[serde(rename = "webrtc_answer")]
    Answer(Answer),
    #[serde(rename = "webrtc_candidate")]
    Candidate(Candidate),
    #[serde(rename = "webrtc_subscribe")]
    Subscribe {
        kind: SubscriptionKind,
        user_id: Id,
        on: bool,
    },
}
#[derive(Clone, Serialize)]
#[serde(tag = "type", content = "payload")]
pub enum MediaMessage {
    #[serde(rename = "offer")]
    Offer(Offer),
    #[serde(rename = "ice")]
    Candidate(Candidate),
}
macro_rules! redacted_debug {($($t:ty),*)=>{$(impl fmt::Debug for $t{fn fmt(&self,f:&mut fmt::Formatter<'_>)->fmt::Result{f.write_str("SyntheticMediaValue(REDACTED)")}})*};}
redacted_debug!(Answer, Offer, Candidate, Action, MediaMessage);
fn sdp_valid(s: &str) -> bool {
    !s.is_empty() && s.len() <= 49152 && !s.contains('\0')
}
pub fn decode_action(input: &[u8]) -> Result<Action, Error> {
    if input.len() > 65536 {
        return Err(Error::BodyLimit);
    }
    let a: Action = serde_json::from_slice(input).map_err(|_| Error::Protocol)?;
    match &a {
        Action::Answer(a) if !sdp_valid(&a.sdp) => return Err(Error::Protocol),
        Action::Candidate(c) => c.validate()?,
        _ => {}
    }
    Ok(a)
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Envelope {
    #[serde(rename = "type")]
    kind: String,
    payload: serde_json::Value,
}
pub fn decode_message(input: &[u8]) -> Result<Option<MediaMessage>, Error> {
    if input.len() > 65536 {
        return Err(Error::BodyLimit);
    }
    let e: Envelope = serde_json::from_slice(input).map_err(|_| Error::Protocol)?;
    match e.kind.as_str() {
        "webrtc_offer" => {
            let p: Offer = serde_json::from_value(e.payload).map_err(|_| Error::Protocol)?;
            if !sdp_valid(&p.sdp) {
                return Err(Error::Protocol);
            }
            Ok(Some(MediaMessage::Offer(p)))
        }
        "webrtc_candidate" => {
            let p: Candidate = serde_json::from_value(e.payload).map_err(|_| Error::Protocol)?;
            p.validate()?;
            Ok(Some(MediaMessage::Candidate(p)))
        }
        "native_renewed" | "native_renew" => Err(Error::Protocol), // Native lease controls must be consumed before this bridge.
        _ => Ok(None), // No content/key/new-control forwarding.
    }
}
#[cfg(feature = "synthetic-media-fixture")]
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct LoginWire {
    user_id: Id,
    username: String,
    password: Zeroizing<String>,
}
#[cfg(feature = "synthetic-media-fixture")]
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct BootstrapWire {
    schema: u8,
    scope: String,
    origin: String,
    ca_pem: String,
    community_id: Id,
    channel_id: Id,
    publisher: LoginWire,
    viewer: LoginWire,
}
pub struct FixtureLogin {
    id: Id,
    username: String,
    password: Zeroizing<String>,
}
impl FixtureLogin {
    pub fn account_id(&self) -> Id {
        self.id
    }
    pub fn username(&self) -> &str {
        &self.username
    }
    pub fn password(&self) -> &str {
        &self.password
    }
}
pub struct OwnedFixture {
    origin: url::Url,
    ca: native_tls::Certificate,
    ca_pem: Vec<u8>,
    ca_fingerprint: [u8; 32],
    community: Id,
    channel: Id,
    publisher: FixtureLogin,
    viewer: FixtureLogin,
}
redacted_debug!(FixtureLogin, OwnedFixture);
impl OwnedFixture {
    pub fn origin(&self) -> &url::Url {
        &self.origin
    }
    pub fn authority(&self) -> native_tls::Certificate {
        self.ca.clone()
    }
    /// Public bytes for the exact validated native-only fixture origin; never OS trust.
    pub fn authority_pem(&self) -> &[u8] {
        &self.ca_pem
    }
    pub fn native_profile(&self, role: Role) -> NativeFixtureProfile<'_> {
        NativeFixtureProfile {
            fixture: self,
            role,
        }
    }
    pub fn authority_fingerprint(&self) -> [u8; 32] {
        self.ca_fingerprint
    }
    pub fn community_id(&self) -> Id {
        self.community
    }
    pub fn channel_id(&self) -> Id {
        self.channel
    }
    pub fn login(&self, role: Role) -> &FixtureLogin {
        match role {
            Role::Publisher => &self.publisher,
            Role::Viewer => &self.viewer,
        }
    }
}
/// Native-only validated profile view, not Deserialize/Serialize/IPC.
/// Broker fixture constructor must bind this exact origin, CA and account; normal
/// product OS trust and user-selected profiles cannot consume this fixture view.
pub struct NativeFixtureProfile<'a> {
    fixture: &'a OwnedFixture,
    role: Role,
}
impl fmt::Debug for NativeFixtureProfile<'_> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeFixtureProfile(REDACTED)")
    }
}
impl NativeFixtureProfile<'_> {
    pub fn origin(&self) -> &url::Url {
        self.fixture.origin()
    }
    pub fn authority_pem(&self) -> &[u8] {
        self.fixture.authority_pem()
    }
    pub fn authority_fingerprint(&self) -> [u8; 32] {
        self.fixture.authority_fingerprint()
    }
    pub fn community_id(&self) -> Id {
        self.fixture.community_id()
    }
    pub fn expected_account(&self) -> Id {
        self.fixture.login(self.role).account_id()
    }
    pub fn username(&self) -> &str {
        self.fixture.login(self.role).username()
    }
    pub fn password(&self) -> &str {
        self.fixture.login(self.role).password()
    }
}
#[cfg(feature = "synthetic-media-fixture")]
fn decode_bootstrap(input: &[u8]) -> Result<OwnedFixture, Error> {
    use sha2::{Digest, Sha256};
    if input.len() > 16384 {
        return Err(Error::BodyLimit);
    }
    let wire: BootstrapWire = serde_json::from_slice(input).map_err(|_| Error::Protocol)?;
    if wire.schema != 1
        || wire.scope != "synthetic native authenticated SFU qualification, no MLS grant"
        || wire.publisher.user_id == wire.viewer.user_id
    {
        return Err(Error::Protocol);
    }
    let origin = url::Url::parse(&wire.origin).map_err(|_| Error::Protocol)?;
    if origin.scheme() != "https"
        || origin.host_str() != Some("127.0.0.1")
        || origin.port().is_none_or(|p| p == 0)
        || origin.path() != "/"
        || !origin.username().is_empty()
        || origin.password().is_some()
        || origin.query().is_some()
        || origin.fragment().is_some()
    {
        return Err(Error::Denied);
    }
    for login in [&wire.publisher, &wire.viewer] {
        if login.password.as_str() != DUMMY_PASSWORD
            || login.username.len() < 3
            || login.username.len() > 32
            || !(login.username.starts_with("nws-") || login.username.starts_with("media-"))
            || !login
                .username
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"_.-".contains(&b))
        {
            return Err(Error::Denied);
        }
    }
    let pem = wire.ca_pem.trim();
    if pem.len() < 128
        || pem.len() > 12288
        || !pem.starts_with("-----BEGIN CERTIFICATE-----\n")
        || !pem.ends_with("\n-----END CERTIFICATE-----")
        || pem.matches("-----BEGIN").count() != 1
        || pem.matches("-----END").count() != 1
        || !pem.is_ascii()
    {
        return Err(Error::Protocol);
    }
    let ca = native_tls::Certificate::from_pem(pem.as_bytes()).map_err(|_| Error::Protocol)?;
    let der = ca.to_der().map_err(|_| Error::Protocol)?;
    let ca_fingerprint = Sha256::digest(&der).into();
    // Exact trust anchor is scoped to this trusted local fixture origin only.
    // Parsing is NOT an independent CA-chain proof: native TLS still verifies leaf,
    // hostname, lifetime and chain for actual HTTPS/WSS; no invalid-cert bypass.
    Ok(OwnedFixture {
        origin,
        ca,
        ca_pem: pem.as_bytes().to_vec(),
        ca_fingerprint,
        community: wire.community_id,
        channel: wire.channel_id,
        publisher: FixtureLogin {
            id: wire.publisher.user_id,
            username: wire.publisher.username,
            password: wire.publisher.password,
        },
        viewer: FixtureLogin {
            id: wire.viewer.user_id,
            username: wire.viewer.username,
            password: wire.viewer.password,
        },
    })
}
pub fn load_owned_bootstrap(path: &Path) -> Result<OwnedFixture, Error> {
    #[cfg(all(unix, feature = "synthetic-media-fixture"))]
    {
        use std::{
            fs::{self, OpenOptions},
            io::Read,
            os::unix::{
                ffi::OsStrExt,
                fs::{MetadataExt, OpenOptionsExt},
            },
        };
        if !path.is_absolute()
            || path
                .file_name()
                .is_none_or(|p| p.as_bytes() != b"bootstrap.json")
        {
            return Err(Error::Denied);
        }
        let parent = path.parent().ok_or(Error::Denied)?;
        let metadata = fs::symlink_metadata(parent).map_err(|_| Error::Denied)?;
        // OS identity, never UID env/input. No secret-bearing fields are logged.
        let uid = unsafe { libc::geteuid() };
        if !metadata.is_dir() || metadata.uid() != uid || metadata.mode() & 0o7777 != 0o700 {
            return Err(Error::Denied);
        }
        let file = OpenOptions::new()
            .read(true)
            .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
            .open(path)
            .map_err(|_| Error::Denied)?;
        let first = file.metadata().map_err(|_| Error::Denied)?;
        if !first.is_file()
            || first.uid() != uid
            || first.nlink() != 1
            || first.mode() & 0o7777 != 0o600
        {
            return Err(Error::Denied);
        }
        if first.len() > 16384 {
            return Err(Error::BodyLimit);
        }
        let mut bytes = Zeroizing::new(Vec::new());
        let mut reader = file.take(16385);
        reader.read_to_end(&mut bytes).map_err(|_| Error::Denied)?;
        if bytes.len() > 16384 {
            return Err(Error::BodyLimit);
        }
        let after = reader.into_inner().metadata().map_err(|_| Error::Denied)?;
        if first.len() != after.len() || first.modified().ok() != after.modified().ok() {
            return Err(Error::Denied);
        }
        decode_bootstrap(&bytes)
    }
    #[cfg(not(all(unix, feature = "synthetic-media-fixture")))]
    {
        let _ = path;
        Err(Error::QualificationRequired)
    }
}
// Native-only evidence, NOT Deserialize/IPC. Caller must obtain these fields from
// checked actual Broker/window/profile/socket grant; this constructor is not auth.
#[derive(Clone, Copy, PartialEq, Eq)]
pub struct NativeBinding {
    pub window_context: Id,
    pub profile_context: Id,
    pub account_id: Id,
    pub family_id: Id,
    pub client_instance_id: Id,
    pub socket_generation: u64,
    pub refresh_sequence: u64,
}
redacted_debug!(NativeBinding);
pub struct FixtureCapability {
    binding: NativeBinding,
    role: Role,
    channel: Id,
    publisher: Id,
    handle: String,
    deadline: Instant,
    sealed: AtomicBool,
}
redacted_debug!(FixtureCapability);
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BoundMedia {
    pub handle: String,
    pub generation: u64,
    pub channel_id: Id,
    pub publisher_user_id: Id,
    pub self_user_id: Id,
}
redacted_debug!(BoundMedia);
impl FixtureCapability {
    #[cfg(feature = "synthetic-media-fixture")]
    pub fn bind(
        fixture: &OwnedFixture,
        role: Role,
        binding: NativeBinding,
        deadline: Instant,
    ) -> Result<Self, Error> {
        if binding.socket_generation == 0
            || binding.socket_generation > 9_007_199_254_740_991
            || binding.refresh_sequence > 65535
            || binding.account_id != fixture.login(role).account_id()
            || deadline <= Instant::now()
            || deadline.saturating_duration_since(Instant::now())
                > std::time::Duration::from_secs(300)
        {
            return Err(Error::Denied);
        }
        Ok(Self {
            binding,
            role,
            channel: fixture.channel,
            publisher: fixture.publisher.id,
            handle: format!("{}{}", Uuid::new_v4().simple(), Uuid::new_v4().simple()),
            deadline,
            sealed: AtomicBool::new(false),
        })
    }
    /// Native actor calls this only after actual native renewal ACK has committed.
    /// Values cannot originate in renderer IPC; module alone does not prove ACK.
    pub fn renew_after_native_ack(
        &mut self,
        current: NativeBinding,
        deadline: Instant,
    ) -> Result<(), Error> {
        let now = Instant::now();
        let mut expected = self.binding;
        expected.refresh_sequence = expected
            .refresh_sequence
            .checked_add(1)
            .ok_or(Error::Denied)?;
        if self.sealed.load(Ordering::Acquire)
            || now >= self.deadline
            || current != expected
            || current.refresh_sequence > 65535
            || deadline <= now
            || deadline.saturating_duration_since(now) > std::time::Duration::from_secs(300)
        {
            return Err(Error::Denied);
        }
        self.binding = current;
        self.deadline = deadline;
        Ok(())
    }
    pub fn seal(&self) {
        self.sealed.store(true, Ordering::Release)
    }
    pub fn check(
        &self,
        current: Option<&NativeBinding>,
        handle: &str,
        generation: u64,
        now: Instant,
    ) -> Result<(), Error> {
        if self.sealed.load(Ordering::Acquire)
            || current != Some(&self.binding)
            || handle != self.handle
            || generation != self.binding.socket_generation
        {
            return Err(Error::Denied);
        }
        if now >= self.deadline {
            return Err(Error::Expired);
        }
        Ok(())
    }
    pub fn bound(&self) -> BoundMedia {
        BoundMedia {
            handle: self.handle.clone(),
            generation: self.binding.socket_generation,
            channel_id: self.channel,
            publisher_user_id: self.publisher,
            self_user_id: self.binding.account_id,
        }
    }
    pub fn authorize(
        &self,
        current: Option<&NativeBinding>,
        handle: &str,
        generation: u64,
        now: Instant,
        action: &Action,
    ) -> Result<(), Error> {
        self.check(current, handle, generation, now)?;
        match action {
            Action::Join { channel_id } if *channel_id != self.channel => Err(Error::Denied),
            Action::Subscribe { user_id, .. }
                if self.role != Role::Viewer || *user_id != self.publisher =>
            {
                Err(Error::Denied)
            }
            Action::Answer(a) if !sdp_valid(&a.sdp) => Err(Error::Protocol),
            Action::Candidate(c) => c.validate(),
            _ => Ok(()),
        }
    }
}
#[derive(Serialize)]
#[serde(tag = "type", rename_all = "lowercase")]
pub enum NativeMediaEvent {
    Message {
        handle: String,
        generation: u64,
        sequence: u64,
        message: MediaMessage,
    },
    Closed {
        handle: String,
        generation: u64,
        sequence: u64,
    },
    Error {
        handle: String,
        generation: u64,
        sequence: u64,
    },
}
redacted_debug!(NativeMediaEvent);
#[derive(Default)]
pub struct EventCursor {
    sequence: u64,
}
impl EventCursor {
    fn next(&mut self) -> Result<u64, Error> {
        let n = self.sequence.checked_add(1).ok_or(Error::Denied)?;
        if n > 9_007_199_254_740_991 {
            return Err(Error::Denied);
        }
        self.sequence = n;
        Ok(n)
    }
    pub fn message(
        &mut self,
        cap: &FixtureCapability,
        current: Option<&NativeBinding>,
        now: Instant,
        message: MediaMessage,
    ) -> Result<NativeMediaEvent, Error> {
        cap.check(current, &cap.handle, cap.binding.socket_generation, now)?;
        match &message {
            MediaMessage::Offer(p) if !sdp_valid(&p.sdp) => return Err(Error::Protocol),
            MediaMessage::Candidate(p) => p.validate()?,
            _ => {}
        }
        Ok(NativeMediaEvent::Message {
            handle: cap.handle.clone(),
            generation: cap.binding.socket_generation,
            sequence: self.next()?,
            message,
        })
    }
    pub fn closed(&mut self, cap: &FixtureCapability) -> Result<NativeMediaEvent, Error> {
        Ok(NativeMediaEvent::Closed {
            handle: cap.handle.clone(),
            generation: cap.binding.socket_generation,
            sequence: self.next()?,
        })
    }
    pub fn error(&mut self, cap: &FixtureCapability) -> Result<NativeMediaEvent, Error> {
        Ok(NativeMediaEvent::Error {
            handle: cap.handle.clone(),
            generation: cap.binding.socket_generation,
            sequence: self.next()?,
        })
    }
}
#[cfg(test)]
mod tests;
