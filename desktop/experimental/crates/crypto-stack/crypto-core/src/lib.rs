//! Ignored qualification; native bootstrap/latest-membership enrollment remain external.
use coset::{CoseSign1, TaggedCborSerializable, cbor::value::Value};
use mnema_crypto_adapter_candidate::Scope;
use mnema_crypto_trust_candidate::{NativeGroupFloor, TrustVerifier, VerifiedRoster};
use openmls::prelude::{tls_codec::Deserialize, *};
use openmls_basic_credential::SignatureKeyPair;
use openmls_rust_crypto::RustCrypto;
use openmls_sqlite_storage::{Codec, SqliteStorageProvider};
use openmls_traits::{OpenMlsProvider, random::OpenMlsRand};
use rusqlite::{Connection, OptionalExtension, TransactionBehavior, params};
use serde::{Serialize, de::DeserializeOwned};
use std::{
    path::Path,
    sync::atomic::{AtomicU64, Ordering},
};
use zeroize::Zeroizing;
const SUITE: Ciphersuite = Ciphersuite::MLS_128_DHKEMX25519_CHACHA20POLY1305_SHA256_Ed25519;
static OWNER: AtomicU64 = AtomicU64::new(1);
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Error {
    Invalid,
    Database,
    Provider,
    KeyUnavailable,
    Trust,
    Replay,
    Restored,
    Quarantined,
    Busy,
    WrongPhase,
    Unauthorized,
    Stale,
    Limit,
    UnknownCommit,
}
pub type Result<T> = std::result::Result<T, Error>;
/// Fixed alias supplied by native owner; no network discovery or key file fallback.
pub trait NativeKeyProvider {
    fn database_key(&self) -> Result<Zeroizing<[u8; 32]>>;
}
#[cfg(target_os = "macos")]
pub struct MacKeychainProvider {
    keychain: security_framework::os::macos::keychain::SecKeychain,
    account: String,
}
#[cfg(target_os = "macos")]
impl MacKeychainProvider {
    pub fn from_native_keychain(
        keychain: security_framework::os::macos::keychain::SecKeychain,
        account: &str,
    ) -> Result<Self> {
        id(account)?;
        Ok(Self {
            keychain,
            account: account.into(),
        })
    }
}
#[cfg(target_os = "macos")]
impl NativeKeyProvider for MacKeychainProvider {
    fn database_key(&self) -> Result<Zeroizing<[u8; 32]>> {
        let (password, _) = self
            .keychain
            .find_generic_password("mnema-talk.crypto-core.prototype", &self.account)
            .map_err(|_| Error::KeyUnavailable)?;
        if password.as_ref().len() != 32 {
            return Err(Error::KeyUnavailable);
        }
        let mut key = Zeroizing::new([0; 32]);
        key.copy_from_slice(password.as_ref());
        Ok(key)
    }
}
#[derive(Default)]
struct JsonCodec;
impl Codec for JsonCodec {
    type Error = serde_json::Error;
    fn to_vec<T: Serialize>(v: &T) -> std::result::Result<Vec<u8>, Self::Error> {
        serde_json::to_vec(v)
    }
    fn from_slice<T: DeserializeOwned>(w: &[u8]) -> std::result::Result<T, Self::Error> {
        serde_json::from_slice(w)
    }
}
struct Provider<'a> {
    crypto: &'a RustCrypto,
    storage: SqliteStorageProvider<JsonCodec, &'a Connection>,
}
impl<'a> Provider<'a> {
    fn new(conn: &'a Connection, crypto: &'a RustCrypto) -> Self {
        Self {
            crypto,
            storage: SqliteStorageProvider::new(conn),
        }
    }
}
impl<'a> OpenMlsProvider for Provider<'a> {
    type CryptoProvider = RustCrypto;
    type RandProvider = RustCrypto;
    type StorageProvider = SqliteStorageProvider<JsonCodec, &'a Connection>;
    fn crypto(&self) -> &RustCrypto {
        self.crypto
    }
    fn rand(&self) -> &RustCrypto {
        self.crypto
    }
    fn storage(&self) -> &Self::StorageProvider {
        &self.storage
    }
}
/// Reviewed native configuration only. Not a server-deserializable record.
pub struct NativeBootstrap {
    scope: Scope,
    group: Vec<u8>,
    authority: [u8; 32],
    peer_identity: Vec<u8>,
    peer_key: [u8; 32],
}
impl NativeBootstrap {
    pub fn from_native_pin(
        scope: Scope,
        group: &[u8],
        authority: [u8; 32],
        peer_identity: &[u8],
        peer_key: [u8; 32],
    ) -> Result<Self> {
        if group.is_empty()
            || group.len() > 128
            || peer_identity.is_empty()
            || peer_identity.len() > 256
        {
            return Err(Error::Invalid);
        }
        let pin = Self {
            scope,
            group: group.into(),
            authority,
            peer_identity: peer_identity.into(),
            peer_key,
        };
        verifier(&pin, 0, 0)?;
        Ok(pin)
    }
}
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Phase {
    FreshOffer,
    AwaitingPeer,
    Live,
    Restored,
    Quarantined,
}
pub struct EnrollmentOffer {
    pub key_package: KeyPackage,
    pub identity: Vec<u8>,
    pub signature_key: Vec<u8>,
    pub challenge: [u8; 32],
}
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct StageHandle {
    owner: u64,
    revision: i64,
}
#[derive(Debug)]
pub struct Inspection {
    pub from_epoch: u64,
    pub to_epoch: u64,
    pub additions: usize,
    pub removals: Vec<u32>,
}
pub struct AuthorizedStage {
    stage: StageHandle,
    generation: u64,
}
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FaultPoint {
    AfterMutation,
    AfterRecord,
    AfterCommit,
}
pub struct Core {
    connection: Connection,
    crypto: RustCrypto,
    pin: NativeBootstrap,
    group: Option<MlsGroup>,
    signer: Option<SignatureKeyPair>,
    challenge: Option<[u8; 32]>,
    phase: Phase,
    revision: i64,
    owner: u64,
    staged: Option<(StageHandle, StagedCommit, Vec<u8>, bool)>,
}
impl Core {
    pub fn begin_enrollment(
        path: &Path,
        keys: &impl NativeKeyProvider,
        pin: NativeBootstrap,
        identity: &[u8],
    ) -> Result<(Self, EnrollmentOffer)> {
        if identity.is_empty() || identity.len() > 256 {
            return Err(Error::Invalid);
        }
        let mut connection = open_encrypted(path, keys, true)?;
        SqliteStorageProvider::<JsonCodec, _>::new(&mut connection)
            .run_migrations()
            .map_err(|_| Error::Database)?;
        schema(&connection, &pin)?;
        let crypto = RustCrypto::default();
        let signer =
            SignatureKeyPair::new(SignatureScheme::ED25519).map_err(|_| Error::Provider)?;
        let challenge = crypto.random_array().map_err(|_| Error::Provider)?;
        let tx = connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        let provider = Provider::new(&tx, &crypto);
        signer
            .store(provider.storage())
            .map_err(|_| Error::Provider)?;
        let bundle = KeyPackage::builder()
            .build(
                SUITE,
                &provider,
                &signer,
                CredentialWithKey {
                    credential: BasicCredential::new(identity.into()).into(),
                    signature_key: signer.to_public_vec().into(),
                },
            )
            .map_err(|_| Error::Provider)?;
        tx.commit().map_err(|_| Error::Database)?;
        let offer = EnrollmentOffer {
            key_package: bundle.key_package().clone(),
            identity: identity.into(),
            signature_key: signer.to_public_vec(),
            challenge,
        };
        Ok((
            Self {
                connection,
                crypto,
                pin,
                group: None,
                signer: Some(signer),
                challenge: Some(challenge),
                phase: Phase::FreshOffer,
                revision: 0,
                owner: owner()?,
                staged: None,
            },
            offer,
        ))
    }
    pub fn restore_inspection_only(
        path: &Path,
        keys: &impl NativeKeyProvider,
        pin: NativeBootstrap,
    ) -> Result<Self> {
        let connection = open_encrypted(path, keys, false)?;
        check_pin(&connection, &pin)?;
        let revision = connection
            .query_row("SELECT revision FROM core_state WHERE id=1", [], |r| {
                r.get(0)
            })
            .map_err(|_| Error::Database)?;
        Ok(Self {
            connection,
            crypto: RustCrypto::default(),
            pin,
            group: None,
            signer: None,
            challenge: None,
            phase: Phase::Restored,
            revision,
            owner: owner()?,
            staged: None,
        })
    }
    pub fn phase(&self) -> Phase {
        self.phase
    }
    fn mutable(&self) -> Result<()> {
        match self.phase {
            Phase::Restored => Err(Error::Restored),
            Phase::Quarantined => Err(Error::Quarantined),
            _ => Ok(()),
        }
    }
    fn quarantine<T>(&mut self, result: Result<T>) -> Result<T> {
        if result.is_err() {
            self.group = None;
            self.signer = None;
            self.challenge = None;
            self.staged = None;
            self.phase = Phase::Quarantined;
        }
        result
    }
    pub fn pending(&self, event: &str) -> Result<Option<Vec<u8>>> {
        id(event)?;
        self.connection
            .query_row(
                "SELECT wire FROM core_outbox WHERE event_id=? AND kind='application'",
                [event],
                |r| r.get(0),
            )
            .optional()
            .map_err(|_| Error::Database)
    }
    pub fn install_roster(&mut self, wire: &[u8], now: u64) -> Result<()> {
        self.mutable()?;
        let result = self.install_roster_inner(wire, now);
        self.quarantine(result)
    }
    fn install_roster_inner(&mut self, wire: &[u8], now: u64) -> Result<()> {
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_revision(&tx, self.revision)?;
        let (floor, last) = stored_floor(&tx)?;
        if now < last {
            return Err(Error::Replay);
        }
        let mut trust = verifier(&self.pin, floor, last)?;
        trust.verify_roster(wire, now).map_err(|_| Error::Trust)?;
        let generation = verified_generation(wire)?;
        tx.execute(
            "UPDATE core_state SET roster=?,generation=?,observed_time=? WHERE id=1",
            params![wire, generation.to_be_bytes().as_slice(), clock(now)?],
        )
        .map_err(|_| Error::Database)?;
        let next = advance(&tx, self.revision, now)?;
        tx.commit().map_err(|_| Error::Database)?;
        self.revision = next;
        Ok(())
    }
    pub fn accept_welcome(&mut self, wire: &[u8], now: u64) -> Result<()> {
        self.mutable()?;
        if self.phase != Phase::FreshOffer {
            return Err(Error::WrongPhase);
        }
        let result = self.welcome_inner(wire, now);
        self.quarantine(result)
    }
    fn welcome_inner(&mut self, wire: &[u8], now: u64) -> Result<()> {
        bound_wire(wire)?;
        let message = MlsMessageIn::tls_deserialize_exact(wire).map_err(|_| Error::Invalid)?;
        let MlsMessageBodyIn::Welcome(welcome) = message.extract() else {
            return Err(Error::Invalid);
        };
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_revision(&tx, self.revision)?;
        let (mut trust, roster, _) = current_trust(&tx, &self.pin, now)?;
        let provider = Provider::new(&tx, &self.crypto);
        let config = MlsGroupJoinConfig::builder()
            .use_ratchet_tree_extension(true)
            .build();
        let staged = StagedWelcome::new_from_welcome(&provider, &config, welcome, None)
            .map_err(|_| Error::Provider)?;
        let members: Vec<_> = staged.members().collect();
        if members.is_empty() || members.len() > 128 {
            return Err(Error::Limit);
        }
        for m in &members {
            approve(
                &mut trust,
                &roster,
                &self.pin,
                m.credential.serialized_content(),
                &m.signature_key,
                now,
            )?;
        }
        let signer = self.signer.as_ref().ok_or(Error::Quarantined)?;
        if !members
            .iter()
            .any(|m| m.signature_key == signer.to_public_vec())
            || !members.iter().any(|m| {
                m.credential.serialized_content() == self.pin.peer_identity
                    && m.signature_key == self.pin.peer_key
            })
        {
            return Err(Error::Unauthorized);
        }
        let group = staged.into_group(&provider).map_err(|_| Error::Provider)?;
        if group.group_id().as_slice() != self.pin.group || !group.is_active() {
            return Err(Error::Unauthorized);
        }
        let next = advance(&tx, self.revision, now)?;
        tx.commit().map_err(|_| Error::Database)?;
        self.group = Some(group);
        self.phase = Phase::AwaitingPeer;
        self.revision = next;
        Ok(())
    }
    /// Nonsecret transcript: the selected approved peer encrypts it as MLS PrivateMessage.
    pub fn peer_challenge_transcript(&self) -> Result<Vec<u8>> {
        if self.phase != Phase::AwaitingPeer {
            return Err(Error::WrongPhase);
        }
        let group = self.group.as_ref().ok_or(Error::Quarantined)?;
        let challenge = self.challenge.ok_or(Error::Quarantined)?;
        let value = Value::Array(vec![
            Value::Text("MnemaTalk FreshJoin/v1".into()),
            Value::Text(self.pin.scope.origin().into()),
            Value::Text(self.pin.scope.community().into()),
            Value::Bytes(self.pin.group.clone()),
            Value::Integer(group.epoch().as_u64().into()),
            Value::Bytes(group.epoch_authenticator().as_slice().into()),
            Value::Bytes(challenge.into()),
        ]);
        let mut wire = Vec::new();
        coset::cbor::ser::into_writer(&value, &mut wire).map_err(|_| Error::Invalid)?;
        Ok(wire)
    }
    pub fn confirm_peer(&mut self, wire: &[u8], now: u64) -> Result<()> {
        self.mutable()?;
        if self.phase != Phase::AwaitingPeer {
            return Err(Error::WrongPhase);
        }
        parse_for(
            self.group.as_ref().ok_or(Error::Quarantined)?,
            wire,
            ContentType::Application,
        )?;
        let expected = self.peer_challenge_transcript()?;
        let result = self.confirm_inner(wire, &expected, now);
        self.quarantine(result)
    }
    fn confirm_inner(&mut self, wire: &[u8], expected: &[u8], now: u64) -> Result<()> {
        let group = self.group.as_mut().ok_or(Error::Quarantined)?;
        let message = parse_for(group, wire, ContentType::Application)?;
        if !matches!(message, ProtocolMessage::PrivateMessage(_)) {
            return Err(Error::Invalid);
        }
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_revision(&tx, self.revision)?;
        let (mut trust, roster, _) = current_trust(&tx, &self.pin, now)?;
        for m in group.members() {
            approve(
                &mut trust,
                &roster,
                &self.pin,
                m.credential.serialized_content(),
                &m.signature_key,
                now,
            )?;
        }
        let processed = group
            .process_message(&Provider::new(&tx, &self.crypto), message)
            .map_err(|_| Error::Provider)?;
        let Sender::Member(index) = processed.sender() else {
            return Err(Error::Unauthorized);
        };
        let member = group
            .members()
            .find(|m| m.index == *index)
            .ok_or(Error::Unauthorized)?;
        if member.credential.serialized_content() != self.pin.peer_identity
            || member.signature_key != self.pin.peer_key
            || processed.credential().serialized_content() != member.credential.serialized_content()
        {
            return Err(Error::Unauthorized);
        }
        let ProcessedMessageContent::ApplicationMessage(app) = processed.into_content() else {
            return Err(Error::Invalid);
        };
        if app.into_bytes() != expected {
            return Err(Error::Unauthorized);
        }
        tx.execute(
            "UPDATE core_state SET fresh_join_epoch=? WHERE id=1",
            [group.epoch().as_u64().to_be_bytes().as_slice()],
        )
        .map_err(|_| Error::Database)?;
        let next = advance(&tx, self.revision, now)?;
        tx.commit().map_err(|_| Error::Database)?;
        self.phase = Phase::Live;
        self.challenge = None;
        self.revision = next;
        Ok(())
    }
    pub fn send(&mut self, event: &str, plaintext: &[u8], now: u64) -> Result<Vec<u8>> {
        self.send_fault(event, plaintext, now, |_| Ok(()))
    }
    fn send_fault(
        &mut self,
        event: &str,
        plaintext: &[u8],
        now: u64,
        hook: impl Fn(FaultPoint) -> Result<()>,
    ) -> Result<Vec<u8>> {
        self.mutable()?;
        if self.phase != Phase::Live {
            return Err(Error::WrongPhase);
        }
        let result = self.send_inner(event, plaintext, now, hook);
        self.quarantine(result)
    }
    fn send_inner(
        &mut self,
        event: &str,
        plaintext: &[u8],
        now: u64,
        hook: impl Fn(FaultPoint) -> Result<()>,
    ) -> Result<Vec<u8>> {
        id(event)?;
        if plaintext.is_empty() || plaintext.len() > 32768 {
            return Err(Error::Invalid);
        }
        if self.staged.is_some() {
            return Err(Error::Busy);
        }
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_revision(&tx, self.revision)?;
        budget(&tx)?;
        if tx
            .query_row(
                "SELECT EXISTS(SELECT 1 FROM core_outbox WHERE event_id=?)",
                [event],
                |r| r.get::<_, bool>(0),
            )
            .map_err(|_| Error::Database)?
        {
            return Err(Error::Replay);
        }
        let (mut trust, roster, _) = current_trust(&tx, &self.pin, now)?;
        let group = self.group.as_mut().ok_or(Error::Quarantined)?;
        if !group.is_active() {
            return Err(Error::Unauthorized);
        }
        for m in group.members() {
            approve(
                &mut trust,
                &roster,
                &self.pin,
                m.credential.serialized_content(),
                &m.signature_key,
                now,
            )?;
        }
        let wire = group
            .create_message(
                &Provider::new(&tx, &self.crypto),
                self.signer.as_ref().ok_or(Error::Quarantined)?,
                plaintext,
            )
            .map_err(|_| Error::Provider)?
            .to_bytes()
            .map_err(|_| Error::Provider)?;
        bound_wire(&wire)?;
        hook(FaultPoint::AfterMutation)?;
        let next = advance(&tx, self.revision, now)?;
        tx.execute(
            "INSERT INTO core_outbox VALUES(?,?,?,?)",
            params![event, next, "application", wire],
        )
        .map_err(|_| Error::Database)?;
        hook(FaultPoint::AfterRecord)?;
        tx.commit().map_err(|_| Error::Database)?;
        hook(FaultPoint::AfterCommit)?;
        self.revision = next;
        Ok(wire)
    }
    pub fn receive_application(&mut self, wire: &[u8], now: u64) -> Result<Vec<u8>> {
        self.mutable()?;
        if self.phase != Phase::Live {
            return Err(Error::WrongPhase);
        }
        if self.staged.is_some() {
            return Err(Error::Busy);
        }
        parse_for(
            self.group.as_ref().ok_or(Error::Quarantined)?,
            wire,
            ContentType::Application,
        )?;
        let result = self.receive_inner(wire, now);
        self.quarantine(result)
    }
    fn receive_inner(&mut self, wire: &[u8], now: u64) -> Result<Vec<u8>> {
        let group = self.group.as_mut().ok_or(Error::Quarantined)?;
        let message = parse_for(group, wire, ContentType::Application)?;
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_revision(&tx, self.revision)?;
        let (mut trust, roster, _) = current_trust(&tx, &self.pin, now)?;
        let processed = group
            .process_message(&Provider::new(&tx, &self.crypto), message)
            .map_err(|_| Error::Provider)?;
        let Sender::Member(index) = processed.sender() else {
            return Err(Error::Unauthorized);
        };
        let member = group
            .members()
            .find(|m| m.index == *index)
            .ok_or(Error::Unauthorized)?;
        approve(
            &mut trust,
            &roster,
            &self.pin,
            member.credential.serialized_content(),
            &member.signature_key,
            now,
        )?;
        if processed.credential().serialized_content() != member.credential.serialized_content() {
            return Err(Error::Unauthorized);
        }
        let ProcessedMessageContent::ApplicationMessage(app) = processed.into_content() else {
            return Err(Error::Invalid);
        };
        let plaintext = app.into_bytes();
        let next = advance(&tx, self.revision, now)?;
        tx.commit().map_err(|_| Error::Database)?;
        self.revision = next;
        Ok(plaintext)
    }
    pub fn stage_commit(&mut self, wire: &[u8], now: u64) -> Result<StageHandle> {
        self.mutable()?;
        if self.phase != Phase::Live {
            return Err(Error::WrongPhase);
        }
        if self.staged.is_some() {
            return Err(Error::Busy);
        }
        parse_for(
            self.group.as_ref().ok_or(Error::Quarantined)?,
            wire,
            ContentType::Commit,
        )?;
        let result = self.stage_inner(wire, now);
        self.quarantine(result)
    }
    fn stage_inner(&mut self, wire: &[u8], now: u64) -> Result<StageHandle> {
        let group = self.group.as_mut().ok_or(Error::Quarantined)?;
        let message = parse_for(group, wire, ContentType::Commit)?;
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_revision(&tx, self.revision)?;
        let (mut trust, roster, _) = current_trust(&tx, &self.pin, now)?;
        let processed = group
            .process_message(&Provider::new(&tx, &self.crypto), message)
            .map_err(|_| Error::Provider)?;
        let Sender::Member(sender) = processed.sender() else {
            return Err(Error::Unauthorized);
        };
        let m = group
            .members()
            .find(|m| m.index == *sender)
            .ok_or(Error::Unauthorized)?;
        approve(
            &mut trust,
            &roster,
            &self.pin,
            m.credential.serialized_content(),
            &m.signature_key,
            now,
        )?;
        if processed.credential().serialized_content() != m.credential.serialized_content() {
            return Err(Error::Unauthorized);
        }
        let sender_identity = m.credential.serialized_content().to_vec();
        let ProcessedMessageContent::StagedCommitMessage(commit) = processed.into_content() else {
            return Err(Error::Invalid);
        };
        let serialized = serde_json::to_vec(&*commit).map_err(|_| Error::Provider)?;
        if serialized.len() > 1048576 {
            return Err(Error::Limit);
        }
        let next = advance(&tx, self.revision, now)?;
        tx.execute(
            "INSERT INTO core_stage VALUES(1,?,?,?)",
            params![next, wire, serialized],
        )
        .map_err(|_| Error::Database)?;
        tx.commit().map_err(|_| Error::Database)?;
        let handle = StageHandle {
            owner: self.owner,
            revision: next,
        };
        self.staged = Some((handle, *commit, sender_identity, false));
        self.revision = next;
        Ok(handle)
    }
    pub fn inspect_stage(&mut self, handle: StageHandle) -> Result<Inspection> {
        self.mutable()?;
        let Some((stored, commit, _, inspected)) = &mut self.staged else {
            return Err(Error::Stale);
        };
        if *stored != handle {
            return Err(Error::Stale);
        }
        *inspected = true;
        Ok(Inspection {
            from_epoch: self
                .group
                .as_ref()
                .ok_or(Error::Quarantined)?
                .epoch()
                .as_u64(),
            to_epoch: commit.epoch().as_u64(),
            additions: commit.add_proposals().count(),
            removals: commit
                .remove_proposals()
                .map(|p| p.remove_proposal().removed().u32())
                .collect(),
        })
    }
    pub fn authorize_stage(&mut self, handle: StageHandle, now: u64) -> Result<AuthorizedStage> {
        self.mutable()?;
        let result = self.authorize_inner(handle, now);
        if matches!(&result, Err(Error::Database | Error::UnknownCommit)) {
            return self.quarantine(result);
        }
        result
    }
    fn authorize_inner(&mut self, handle: StageHandle, now: u64) -> Result<AuthorizedStage> {
        let (stored, commit, sender, inspected) = self.staged.as_ref().ok_or(Error::Stale)?;
        if *stored != handle || !*inspected {
            return Err(Error::Stale);
        }
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_revision(&tx, self.revision)?;
        let (mut trust, roster, generation) = current_trust(&tx, &self.pin, now)?;
        authorize(
            &mut trust,
            &roster,
            &self.pin,
            self.group.as_ref().ok_or(Error::Quarantined)?,
            commit,
            sender,
            now,
        )?;
        let next = advance(&tx, self.revision, now)?;
        tx.commit().map_err(|_| Error::Database)?;
        self.revision = next;
        Ok(AuthorizedStage {
            stage: handle,
            generation,
        })
    }
    pub fn merge_authorized(
        &mut self,
        authorized: AuthorizedStage,
        event: &str,
        now: u64,
    ) -> Result<()> {
        self.mutable()?;
        let result = self.merge_inner(authorized, event, now);
        self.quarantine(result)
    }
    fn merge_inner(&mut self, authorized: AuthorizedStage, event: &str, now: u64) -> Result<()> {
        id(event)?;
        let (handle, commit, sender, inspected) = self.staged.take().ok_or(Error::Stale)?;
        if handle != authorized.stage || !inspected {
            return Err(Error::Stale);
        }
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_revision(&tx, self.revision)?;
        budget(&tx)?;
        let (mut trust, roster, generation) = current_trust(&tx, &self.pin, now)?;
        if generation != authorized.generation {
            return Err(Error::Stale);
        }
        let group = self.group.as_mut().ok_or(Error::Quarantined)?;
        authorize(&mut trust, &roster, &self.pin, group, &commit, &sender, now)?;
        let wire: Vec<u8> = tx
            .query_row(
                "SELECT wire FROM core_stage WHERE id=1 AND revision=?",
                [handle.revision],
                |r| r.get(0),
            )
            .map_err(|_| Error::Stale)?;
        group
            .merge_staged_commit(&Provider::new(&tx, &self.crypto), commit)
            .map_err(|_| Error::Provider)?;
        let next = advance(&tx, self.revision, now)?;
        tx.execute(
            "INSERT INTO core_outbox VALUES(?,?,?,?)",
            params![event, next, "accepted-commit", wire],
        )
        .map_err(|_| Error::Database)?;
        tx.execute("DELETE FROM core_stage WHERE id=1", [])
            .map_err(|_| Error::Database)?;
        tx.commit().map_err(|_| Error::Database)?;
        self.revision = next;
        if !group.is_active() {
            self.group = None;
            self.signer = None;
            self.phase = Phase::Quarantined;
        }
        Ok(())
    }
}
fn owner() -> Result<u64> {
    OWNER
        .try_update(Ordering::Relaxed, Ordering::Relaxed, |n| n.checked_add(1))
        .map_err(|_| Error::Limit)
}
fn id(v: &str) -> Result<()> {
    if v.is_empty()
        || v.len() > 128
        || !v
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"._:-".contains(&b))
    {
        Err(Error::Invalid)
    } else {
        Ok(())
    }
}
fn bound_wire(w: &[u8]) -> Result<()> {
    if w.is_empty() || w.len() > 65536 {
        Err(Error::Invalid)
    } else {
        Ok(())
    }
}
fn clock(now: u64) -> Result<i64> {
    i64::try_from(now).map_err(|_| Error::Invalid)
}
fn open_encrypted(path: &Path, keys: &impl NativeKeyProvider, create: bool) -> Result<Connection> {
    let key = keys.database_key()?;
    if create {
        let mut options = std::fs::OpenOptions::new();
        options.write(true).create_new(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            options.mode(0o600);
        }
        options.open(path).map_err(|_| Error::Database)?;
    } else {
        let metadata = std::fs::symlink_metadata(path).map_err(|_| Error::Database)?;
        if !metadata.is_file() || metadata.len() > 64 * 1024 * 1024 {
            return Err(Error::Invalid);
        }
    }
    let conn = Connection::open_with_flags(
        path,
        rusqlite::OpenFlags::SQLITE_OPEN_READ_WRITE
            | rusqlite::OpenFlags::SQLITE_OPEN_NO_MUTEX
            | rusqlite::OpenFlags::SQLITE_OPEN_NOFOLLOW,
    )
    .map_err(|_| Error::Database)?;
    let mut raw = Zeroizing::new(String::from("x'"));
    use std::fmt::Write;
    for byte in key.iter() {
        write!(&mut *raw, "{byte:02x}").map_err(|_| Error::Invalid)?;
    }
    raw.push('\'');
    conn.pragma_update(None, "key", raw.as_str())
        .map_err(|_| Error::Database)?;
    let version: String = conn
        .pragma_query_value(None, "cipher_version", |r| r.get(0))
        .map_err(|_| Error::Database)?;
    if !version.starts_with("4.") {
        return Err(Error::Database);
    }
    conn.query_row("SELECT COUNT(*) FROM sqlite_master", [], |r| {
        r.get::<_, i64>(0)
    })
    .map_err(|_| Error::Database)?;
    conn.execute_batch("PRAGMA cipher_memory_security=ON;PRAGMA cipher_plaintext_header_size=0;PRAGMA journal_mode=DELETE;PRAGMA synchronous=EXTRA;PRAGMA fullfsync=ON;PRAGMA foreign_keys=ON;PRAGMA busy_timeout=100;PRAGMA temp_store=MEMORY;").map_err(|_|Error::Database)?;
    for (name, expected) in [
        ("synchronous", 3),
        ("cipher_use_hmac", 1),
        ("cipher_plaintext_header_size", 0),
        ("cipher_memory_security", 1),
        ("fullfsync", 1),
        ("foreign_keys", 1),
        ("temp_store", 2),
    ] {
        let actual = pragma_number(&conn, name)?;
        if actual != expected {
            return Err(Error::Database);
        }
    }
    Ok(conn)
}
fn pragma_number(conn: &Connection, name: &str) -> Result<i64> {
    let value: rusqlite::types::Value = conn
        .pragma_query_value(None, name, |r| r.get(0))
        .map_err(|_| Error::Database)?;
    match value {
        rusqlite::types::Value::Integer(number) => Ok(number),
        rusqlite::types::Value::Text(text) if text.len() < 20 => {
            text.parse().map_err(|_| Error::Database)
        }
        _ => Err(Error::Database),
    }
}
fn schema(conn: &Connection, p: &NativeBootstrap) -> Result<()> {
    conn.execute_batch("CREATE TABLE core_state(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL CHECK(revision>=0),generation BLOB NOT NULL CHECK(length(generation)=8),observed_time INTEGER NOT NULL CHECK(observed_time>=0),roster BLOB CHECK(length(roster)<=131072),fresh_join_epoch BLOB);CREATE TABLE core_pin(origin TEXT NOT NULL,community TEXT NOT NULL,group_id BLOB NOT NULL,authority BLOB NOT NULL,peer_identity BLOB NOT NULL,peer_key BLOB NOT NULL);CREATE TABLE core_outbox(event_id TEXT PRIMARY KEY CHECK(length(event_id) BETWEEN 1 AND 128),revision INTEGER NOT NULL,kind TEXT NOT NULL,wire BLOB NOT NULL CHECK(length(wire) BETWEEN 1 AND 65536));CREATE TABLE core_stage(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL,wire BLOB NOT NULL CHECK(length(wire) BETWEEN 1 AND 65536),stage BLOB NOT NULL CHECK(length(stage) BETWEEN 1 AND 1048576));").map_err(|_|Error::Database)?;
    conn.execute(
        "INSERT INTO core_state VALUES(1,0,?,0,NULL,NULL)",
        [0u64.to_be_bytes().as_slice()],
    )
    .map_err(|_| Error::Database)?;
    conn.execute(
        "INSERT INTO core_pin VALUES(?,?,?,?,?,?)",
        params![
            p.scope.origin(),
            p.scope.community(),
            p.group,
            p.authority.as_slice(),
            p.peer_identity,
            p.peer_key.as_slice()
        ],
    )
    .map_err(|_| Error::Database)?;
    Ok(())
}
fn check_pin(conn: &Connection, p: &NativeBootstrap) -> Result<()> {
    let matched:bool=conn.query_row("SELECT COUNT(*)=1 FROM core_pin WHERE origin=? AND community=? AND group_id=? AND authority=? AND peer_identity=? AND peer_key=?",params![p.scope.origin(),p.scope.community(),p.group,p.authority.as_slice(),p.peer_identity,p.peer_key.as_slice()],|r|r.get(0)).map_err(|_|Error::Database)?;
    if matched { Ok(()) } else { Err(Error::Trust) }
}
fn check_revision(conn: &Connection, expected: i64) -> Result<()> {
    let actual: i64 = conn
        .query_row("SELECT revision FROM core_state WHERE id=1", [], |r| {
            r.get(0)
        })
        .map_err(|_| Error::Database)?;
    if actual == expected {
        Ok(())
    } else {
        Err(Error::Stale)
    }
}
fn advance(conn: &Connection, expected: i64, now: u64) -> Result<i64> {
    let next = expected.checked_add(1).ok_or(Error::Limit)?;
    let count = conn
        .execute(
            "UPDATE core_state SET revision=?,observed_time=? WHERE id=1 AND revision=?",
            params![next, clock(now)?, expected],
        )
        .map_err(|_| Error::Database)?;
    if count == 1 {
        Ok(next)
    } else {
        Err(Error::Stale)
    }
}
fn stored_floor(conn: &Connection) -> Result<(u64, u64)> {
    let (bytes, time): (Vec<u8>, i64) = conn
        .query_row(
            "SELECT generation,observed_time FROM core_state WHERE id=1",
            [],
            |r| Ok((r.get(0)?, r.get(1)?)),
        )
        .map_err(|_| Error::Database)?;
    let bytes: [u8; 8] = bytes.try_into().map_err(|_| Error::Database)?;
    Ok((
        u64::from_be_bytes(bytes),
        u64::try_from(time).map_err(|_| Error::Database)?,
    ))
}
fn verifier(p: &NativeBootstrap, floor: u64, now: u64) -> Result<TrustVerifier> {
    TrustVerifier::from_native_pin(
        p.scope.clone(),
        &p.authority,
        vec![NativeGroupFloor::from_native_store(&p.group, floor).map_err(|_| Error::Trust)?],
        now,
    )
    .map_err(|_| Error::Trust)
}
fn verified_generation(wire: &[u8]) -> Result<u64> {
    /* AFTER frozen bounded verifier accepted these exact bytes. */
    let signed = CoseSign1::from_tagged_slice(wire).map_err(|_| Error::Trust)?;
    let payload = signed.payload.ok_or(Error::Trust)?;
    let value: Value =
        coset::cbor::de::from_reader(payload.as_slice()).map_err(|_| Error::Trust)?;
    let Value::Array(fields) = value else {
        return Err(Error::Trust);
    };
    let Some(Value::Integer(generation)) = fields.get(5) else {
        return Err(Error::Trust);
    };
    u64::try_from(*generation).map_err(|_| Error::Trust)
}
fn current_trust(
    conn: &Connection,
    p: &NativeBootstrap,
    now: u64,
) -> Result<(TrustVerifier, VerifiedRoster, u64)> {
    let (generation, last) = stored_floor(conn)?;
    if generation == 0 || now < last {
        return Err(Error::Trust);
    }
    let wire: Vec<u8> = conn
        .query_row("SELECT roster FROM core_state WHERE id=1", [], |r| r.get(0))
        .map_err(|_| Error::Trust)?;
    if wire.len() > 131072 {
        return Err(Error::Trust);
    } /* Reverify stored gen with floor gen-1; never updates floor, never activates restored state. */
    let mut trust = verifier(p, generation - 1, last)?;
    let roster = trust.verify_roster(&wire, now).map_err(|_| Error::Trust)?;
    if verified_generation(&wire)? != generation {
        return Err(Error::Trust);
    }
    Ok((trust, roster, generation))
}
fn approve(
    t: &mut TrustVerifier,
    r: &VerifiedRoster,
    p: &NativeBootstrap,
    identity: &[u8],
    key: &[u8],
    now: u64,
) -> Result<()> {
    t.verify_device(r, &p.group, identity, key, now)
        .map(|_| ())
        .map_err(|_| Error::Unauthorized)
}
fn budget(conn: &Connection) -> Result<()> {
    let (count, bytes): (i64, i64) = conn
        .query_row(
            "SELECT COUNT(*),COALESCE(SUM(length(wire)),0) FROM core_outbox",
            [],
            |r| Ok((r.get(0)?, r.get(1)?)),
        )
        .map_err(|_| Error::Database)?;
    if count >= 256 || bytes > 8 * 1024 * 1024 - 65536 {
        Err(Error::Limit)
    } else {
        Ok(())
    }
}
fn parse_for(group: &MlsGroup, wire: &[u8], expected: ContentType) -> Result<ProtocolMessage> {
    bound_wire(wire)?;
    let message = MlsMessageIn::tls_deserialize_exact(wire)
        .map_err(|_| Error::Invalid)?
        .try_into_protocol_message()
        .map_err(|_| Error::Invalid)?;
    if message.content_type() != expected
        || message.group_id() != group.group_id()
        || message.epoch() != group.epoch()
    {
        Err(Error::Invalid)
    } else {
        Ok(message)
    }
}
fn authorize(
    t: &mut TrustVerifier,
    r: &VerifiedRoster,
    p: &NativeBootstrap,
    group: &MlsGroup,
    commit: &StagedCommit,
    sender: &[u8],
    now: u64,
) -> Result<()> {
    if commit.queued_proposals().any(|q| {
        !matches!(
            q.proposal(),
            Proposal::Add(_) | Proposal::Update(_) | Proposal::Remove(_)
        )
    }) {
        return Err(Error::Unauthorized);
    }
    let removed: Vec<_> = commit
        .remove_proposals()
        .map(|q| q.remove_proposal().removed())
        .collect();
    let members: Vec<_> = group.members().collect();
    let committer = members
        .iter()
        .find(|m| m.credential.serialized_content() == sender)
        .ok_or(Error::Unauthorized)?;
    approve(
        t,
        r,
        p,
        committer.credential.serialized_content(),
        &committer.signature_key,
        now,
    )?;
    if members.len().saturating_add(commit.add_proposals().count()) > 128 {
        return Err(Error::Limit);
    }
    for m in &members {
        if !removed.contains(&m.index) {
            approve(
                t,
                r,
                p,
                m.credential.serialized_content(),
                &m.signature_key,
                now,
            )?;
        }
    }
    let leaf_check = |t: &mut TrustVerifier, leaf: &LeafNode| -> Result<()> {
        if leaf.credential().credential_type() != CredentialType::Basic {
            return Err(Error::Unauthorized);
        }
        approve(
            t,
            r,
            p,
            leaf.credential().serialized_content(),
            leaf.signature_key().as_slice(),
            now,
        )
    };
    for q in commit.add_proposals() {
        let leaf = q.add_proposal().key_package().leaf_node();
        if members
            .iter()
            .any(|m| m.credential.serialized_content() == leaf.credential().serialized_content())
        {
            return Err(Error::Unauthorized);
        }
        leaf_check(t, leaf)?;
    }
    for q in commit.update_proposals() {
        let Sender::Member(index) = q.sender() else {
            return Err(Error::Unauthorized);
        };
        let old = members
            .iter()
            .find(|m| m.index == *index)
            .ok_or(Error::Unauthorized)?;
        let leaf = q.update_proposal().leaf_node();
        if old.credential.serialized_content() != leaf.credential().serialized_content() {
            return Err(Error::Unauthorized);
        }
        leaf_check(t, leaf)?;
    }
    if let Some(leaf) = commit.update_path_leaf_node() {
        if leaf.credential().serialized_content() != sender {
            return Err(Error::Unauthorized);
        }
        leaf_check(t, leaf)?;
    }
    Ok(())
}
#[cfg(test)]
mod tests;
