use super::*;
use sframe::{
    CipherSuite,
    crypto::{DecryptionBufferView, EncryptionBufferView},
    frame::{
        FrameCounter, MonotonicCounter,
        validation::{FrameValidation, ReplayAttackProtectionStore, Tolerance, UnvalidatedFrame},
    },
    header::SframeHeader,
    key::{DecryptionKey, EncryptionKey},
};
use std::cell::Cell;
use std::collections::{HashMap, HashSet};

const CHAT_DOMAIN: &str = "MnemaTalk ProtectedChat";
const SOURCE_DOMAIN: &str = "MnemaTalk ProtectedSource";
const VOICE_SOURCE_DOMAIN: &str = "MnemaTalk ProtectedVoiceSource";
const MAX_BODY: usize = 24 * 1024;
/// Native actor policy input. Shape checks do not authenticate the actor or root.
pub struct NativeBinding {
    channel: String,
    profile: String,
    window: String,
    session: String,
    account: String,
    device: String,
}
impl NativeBinding {
    pub(super) fn channel_for_native_host(&self) -> &str {
        &self.channel
    }
    pub fn from_native_actor(
        channel: &str,
        profile: &str,
        window: &str,
        session: &str,
        account: &str,
        device: &str,
    ) -> Result<Self> {
        uuid(channel)?;
        for item in [profile, window, session, account, device] {
            id(item)?;
        }
        Ok(Self {
            channel: channel.into(),
            profile: profile.into(),
            window: window.into(),
            session: session.into(),
            account: account.into(),
            device: device.into(),
        })
    }
}
pub struct Sdk {
    pub(super) core: Core,
    binding: NativeBinding,
    retired: bool,
    spent: HashSet<(u64, u32, u16)>,
    received: HashSet<(u64, u32, u16)>,
    bridges: HashMap<u64, FrameBridge>,
    observed_native_time: Cell<u64>,
}
pub struct ChatMessage {
    pub event_id: String,
    pub account: String,
    pub device: String,
    pub body: String,
}
pub(super) struct Receipt {
    pub plaintext: Vec<u8>,
    pub account: String,
    pub device: String,
    pub sender: u32,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SourceKind {
    Opus,
    Vp8,
}
impl SourceKind {
    fn name(self) -> &'static str {
        match self {
            Self::Opus => "opus",
            Self::Vp8 => "vp8",
        }
    }
}
/// Not deserializable or renderer-minted. A context remains burned after lease end.
pub struct SourceLease {
    owner: u64,
    epoch: u64,
    generation: u64,
    sender: u32,
    context: u16,
    source: String,
    kind: SourceKind,
    sending: bool,
    announcement: Vec<u8>,
}
impl SourceLease {
    pub fn announcement_for_native_relay(&self) -> &[u8] {
        &self.announcement
    }
    #[cfg(test)]
    pub(super) fn fixture_duplicate(&self) -> Self {
        Self {
            owner: self.owner,
            epoch: self.epoch,
            generation: self.generation,
            sender: self.sender,
            context: self.context,
            source: self.source.clone(),
            kind: self.kind,
            sending: self.sending,
            announcement: self.announcement.clone(),
        }
    }
}
/// Native-only material. No Serialize, Debug, key getters or renderer export.
/// This alone does not qualify any JS worker delivery or worker attestation.
pub struct WorkerMaterial {
    base: Zeroizing<Vec<u8>>,
    kid: u64,
    kind: SourceKind,
    sending: bool,
}
pub struct NativeFrameGrant {
    owner: u64,
    serial: u64,
}
enum Direction {
    Send(EncryptionKey, MonotonicCounter),
    Receive(DecryptionKey, ReplayAttackProtectionStore),
}
struct FrameBridge {
    epoch: u64,
    generation: u64,
    codec: SourceKind,
    kid: u64,
    direction: Direction,
}
impl FrameBridge {
    fn transform(&mut self, input: &[u8]) -> Result<Vec<u8>> {
        if input.is_empty() || input.len() > 2 * 1024 * 1024 {
            return Err(Error::Invalid);
        }
        let skip = match self.codec {
            SourceKind::Opus => 1,
            SourceKind::Vp8 => {
                if input[0] & 1 == 0 {
                    10
                } else {
                    3
                }
            }
        };
        if input.len() < skip {
            return Err(Error::Invalid);
        }
        match &mut self.direction {
            Direction::Send(key, counter) => {
                // Reserve enough bounded packet room for the RFC header/tag so
                // maximum accepted send output remains accepted by receive.
                if input.len() > 2 * 1024 * 1024 - 64 {
                    return Err(Error::Limit);
                }
                let count = counter.try_next().map_err(|_| Error::Limit)?;
                let header = SframeHeader::new(self.kid, count);
                let mut serialized = vec![0; header.len()];
                header
                    .serialize(&mut serialized)
                    .map_err(|_| Error::Provider)?;
                let mut aad = [serialized.as_slice(), &input[..skip]].concat();
                let mut data = input[skip..].to_vec();
                let mut tag = vec![0; 16];
                key.encrypt(
                    EncryptionBufferView {
                        aad: &mut aad,
                        data: &mut data,
                        tag: &mut tag,
                    },
                    count,
                )
                .map_err(|_| Error::Provider)?;
                Ok([
                    &input[..skip],
                    serialized.as_slice(),
                    data.as_slice(),
                    tag.as_slice(),
                ]
                .concat())
            }
            Direction::Receive(key, replay) => {
                let wire = &input[skip..];
                let header = SframeHeader::deserialize(wire).map_err(|_| Error::Invalid)?;
                if header.key_id() != self.kid || wire.len() < header.len() + 16 {
                    return Err(Error::Unauthorized);
                }
                let token = replay
                    .screen(UnvalidatedFrame::new(&header, &input[..skip]))
                    .map_err(|_| Error::Replay)?;
                let mut aad = [&wire[..header.len()], &input[..skip]].concat();
                let mut data = wire[header.len()..].to_vec();
                key.decrypt(
                    DecryptionBufferView {
                        aad: &mut aad,
                        data: &mut data,
                    },
                    header.counter(),
                )
                .map_err(|_| Error::Unauthorized)?;
                data.truncate(data.len() - 16);
                replay.record(token);
                Ok([&input[..skip], data.as_slice()].concat())
            }
        }
    }
}
impl WorkerMaterial {
    pub fn key_size_for_native(&self) -> usize {
        self.base.len()
    }
    pub fn key_id_for_native(&self) -> u64 {
        self.kid
    }
    pub fn codec_for_native(&self) -> SourceKind {
        self.kind
    }
    pub fn sends_for_native(&self) -> bool {
        self.sending
    }
    #[cfg(test)]
    pub(super) fn fixture_key(&self) -> &[u8] {
        &self.base
    }
}

impl Sdk {
    pub fn install_native_roster(&mut self, wire: &[u8], now: u64) -> Result<()> {
        if self.retired {
            return Err(Error::Stale);
        }
        let result = self.core.install_roster(wire, now);
        self.bridges.clear();
        if result.is_err() {
            self.retire_native();
        }
        // Keep burned/received contexts even at an unchanged MLS epoch.
        result
    }
    pub fn stage_native_commit(&mut self, wire: &[u8], now: u64) -> Result<StageHandle> {
        if self.retired {
            return Err(Error::Stale);
        }
        let result = self.core.stage_commit(wire, now);
        if result.is_ok() || self.core.phase == Phase::Quarantined {
            self.bridges.clear();
        }
        result
    }
    pub fn inspect_native_commit(&mut self, handle: StageHandle) -> Result<Inspection> {
        if self.retired {
            return Err(Error::Stale);
        }
        self.core.inspect_stage(handle)
    }
    pub fn authorize_native_commit(
        &mut self,
        handle: StageHandle,
        now: u64,
    ) -> Result<AuthorizedStage> {
        if self.retired {
            return Err(Error::Stale);
        }
        self.core.authorize_stage(handle, now)
    }
    pub fn merge_native_commit(
        &mut self,
        approval: AuthorizedStage,
        event: &str,
        now: u64,
    ) -> Result<()> {
        if self.retired {
            return Err(Error::Stale);
        }
        let result = self.core.merge_authorized(approval, event, now);
        self.bridges.clear();
        if result.is_err() || self.core.phase == Phase::Quarantined {
            self.retire_native();
        }
        result
    }
    #[cfg(test)]
    pub(super) fn bridges_fixture_count(&self) -> usize {
        self.bridges.len()
    }
    pub fn activate_native_frame_bridge(
        &mut self,
        lease: SourceLease,
        now: u64,
    ) -> Result<NativeFrameGrant> {
        if self.bridges.len() >= 64 {
            return Err(Error::Limit);
        }
        let epoch = lease.epoch;
        let generation = lease.generation;
        let material = self.consume_for_native_worker(lease, now)?;
        let suite = CipherSuite::AesGcm256Sha512;
        let direction = if material.sending {
            Direction::Send(
                EncryptionKey::derive_from(suite, material.kid, material.base.as_slice())
                    .map_err(|_| Error::Provider)?,
                MonotonicCounter::default(),
            )
        } else {
            Direction::Receive(
                DecryptionKey::derive_from(suite, material.kid, material.base.as_slice())
                    .map_err(|_| Error::Provider)?,
                ReplayAttackProtectionStore::new(Tolerance::new(128)),
            )
        };
        let serial = owner()?;
        self.bridges.insert(
            serial,
            FrameBridge {
                epoch,
                generation,
                codec: material.kind,
                kid: material.kid,
                direction,
            },
        );
        Ok(NativeFrameGrant {
            owner: self.core.owner,
            serial,
        })
    }
    /// Native IPC must bind calling profile/window/session before invoking this.
    /// No key material enters renderer/worker JS; plaintext frame processing is native.
    pub fn check_native_frame_bridge(&self, grant: &NativeFrameGrant, now: u64) -> Result<()> {
        if grant.owner != self.core.owner {
            return Err(Error::Stale);
        }
        let (epoch, generation, _, _, _) = self.current(now)?;
        let bridge = self.bridges.get(&grant.serial).ok_or(Error::Stale)?;
        if bridge.epoch != epoch || bridge.generation != generation {
            return Err(Error::Stale);
        }
        Ok(())
    }
    pub fn transform_native_frame(
        &mut self,
        grant: &NativeFrameGrant,
        input: &[u8],
        now: u64,
    ) -> Result<Vec<u8>> {
        if grant.owner != self.core.owner {
            return Err(Error::Stale);
        }
        let (epoch, generation, _, _, _) = self.current(now)?;
        let bridge = self.bridges.get_mut(&grant.serial).ok_or(Error::Stale)?;
        if bridge.epoch != epoch || bridge.generation != generation {
            self.bridges.remove(&grant.serial);
            return Err(Error::Stale);
        }
        bridge.transform(input)
    }
    pub fn stop_native_frame_bridge(&mut self, grant: NativeFrameGrant) -> Result<()> {
        if grant.owner != self.core.owner || self.bridges.remove(&grant.serial).is_none() {
            return Err(Error::Stale);
        }
        Ok(())
    }
    pub fn bind_native(core: Core, binding: NativeBinding, now: u64) -> Result<Self> {
        let is_host:i64=core.connection.query_row("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='native_host_channel'",[],|r|r.get(0)).map_err(|_|Error::Database)?;
        if is_host == 1 {
            let channel: String = core
                .connection
                .query_row(
                    "SELECT channel FROM native_host_channel WHERE id=1",
                    [],
                    |r| r.get(0),
                )
                .map_err(|_| Error::Database)?;
            if channel != binding.channel {
                return Err(Error::Trust);
            }
        }

        let sdk = Self {
            core,
            binding,
            retired: false,
            spent: HashSet::new(),
            received: HashSet::new(),
            bridges: HashMap::new(),
            observed_native_time: Cell::new(0),
        };
        sdk.current(now)?;
        Ok(sdk)
    }
    /// Native logout, disconnect, profile/window change or broker invalidation.
    pub fn retire_native(&mut self) {
        self.retired = true;
        self.core.group = None;
        self.core.signer = None;
        self.core.challenge = None;
        self.core.phase = Phase::Quarantined;
        self.spent.clear();
        self.received.clear();
        self.bridges.clear();
    }
    pub fn matches_native_actor(&self, profile: &str, window: &str, session: &str) -> bool {
        !self.retired
            && self.binding.profile == profile
            && self.binding.window == window
            && self.binding.session == session
    }
    pub(super) fn current(&self, now: u64) -> Result<(u64, u64, u32, String, String)> {
        if self.retired {
            return Err(Error::Stale);
        }
        if now < self.observed_native_time.get() {
            return Err(Error::Replay);
        }
        // Single native actor owner. Retain observed time even when a later trust
        // check expires; process restart cannot activate a restored owner.
        self.observed_native_time.set(now);
        self.core.mutable()?;
        if self.core.phase != Phase::Live {
            return Err(Error::WrongPhase);
        }
        if self.core.staged.is_some() {
            return Err(Error::Busy);
        }
        check_revision(&self.core.connection, self.core.revision)?;
        let (mut trust, roster, generation) =
            current_trust(&self.core.connection, &self.core.pin, now)?;
        let group = self.core.group.as_ref().ok_or(Error::Quarantined)?;
        if !group.is_active() {
            return Err(Error::Unauthorized);
        }
        for member in group.members() {
            approve(
                &mut trust,
                &roster,
                &self.core.pin,
                member.credential.serialized_content(),
                &member.signature_key,
                now,
            )?;
        }
        let own = group
            .members()
            .find(|m| m.index == group.own_leaf_index())
            .ok_or(Error::Unauthorized)?;
        let approved = trust
            .verify_device(
                &roster,
                &self.core.pin.group,
                own.credential.serialized_content(),
                &own.signature_key,
                now,
            )
            .map_err(|_| Error::Unauthorized)?;
        if approved.account() != self.binding.account || approved.device() != self.binding.device {
            return Err(Error::Unauthorized);
        }
        Ok((
            group.epoch().as_u64(),
            generation,
            own.index.u32(),
            approved.account().into(),
            approved.device().into(),
        ))
    }
    pub fn send_chat(&mut self, event_id: &str, body: &str, now: u64) -> Result<Vec<u8>> {
        uuid(event_id)?;
        if body.is_empty() || body.len() > MAX_BODY {
            return Err(Error::Invalid);
        }
        let (epoch, generation, _, account, device) = self.current(now)?;
        let wire = encode(&self.envelope(
            CHAT_DOMAIN,
            event_id,
            &account,
            &device,
            Value::Text(body.into()),
        ))?;
        let result = self.core.send_inner(
            event_id,
            &wire,
            now,
            |_| Ok(()),
            Some((&self.binding.channel, epoch, generation)),
        );
        self.core.quarantine(result)
    }
    pub fn pending_chat_for_native_publish(
        &self,
        event_id: &str,
        now: u64,
    ) -> Result<Option<Vec<u8>>> {
        uuid(event_id)?;
        let (epoch, generation, _, _, _) = self.current(now)?;
        let matched:bool=self.core.connection.query_row("SELECT EXISTS(SELECT 1 FROM sdk_events WHERE event=? AND channel=? AND epoch=? AND generation=?)",params![event_id,self.binding.channel,epoch.to_be_bytes().as_slice(),generation.to_be_bytes().as_slice()],|r|r.get(0)).map_err(|_|Error::Database)?;
        if !matched {
            return Err(Error::Stale);
        }
        self.core.pending(event_id)
    }
    pub fn receive_chat(&mut self, event_id: &str, wire: &[u8], now: u64) -> Result<ChatMessage> {
        uuid(event_id)?;
        self.current(now)?;
        let result = (|| {
            let receipt = self.core.receive_inner(wire, now)?;
            let payload = self.checked_envelope(CHAT_DOMAIN, event_id, &receipt)?;
            let Value::Text(body) = payload else {
                return Err(Error::Invalid);
            };
            if body.is_empty() || body.len() > MAX_BODY {
                return Err(Error::Invalid);
            }
            Ok(ChatMessage {
                event_id: event_id.into(),
                account: receipt.account,
                device: receipt.device,
                body,
            })
        })();
        if result.is_err() {
            self.retire_native();
        }
        result
    }
    fn envelope(
        &self,
        domain: &str,
        event: &str,
        account: &str,
        device: &str,
        payload: Value,
    ) -> Value {
        Value::Array(vec![
            Value::Text(domain.into()),
            Value::Integer(1.into()),
            Value::Text(self.core.pin.scope.origin().into()),
            Value::Text(self.core.pin.scope.community().into()),
            Value::Text(self.binding.channel.clone()),
            Value::Bytes(self.core.pin.group.clone()),
            Value::Text(account.into()),
            Value::Text(device.into()),
            Value::Text(event.into()),
            payload,
        ])
    }
    fn checked_envelope(&self, domain: &str, event: &str, receipt: &Receipt) -> Result<Value> {
        scan(&receipt.plaintext)?;
        let value: Value = coset::cbor::de::from_reader(receipt.plaintext.as_slice())
            .map_err(|_| Error::Invalid)?;
        if encode(&value)? != receipt.plaintext {
            return Err(Error::Invalid);
        }
        let Value::Array(mut fields) = value else {
            return Err(Error::Invalid);
        };
        if fields.len() != 10 {
            return Err(Error::Invalid);
        }
        let payload = fields.pop().ok_or(Error::Invalid)?;
        let expected = self.envelope(
            domain,
            event,
            &receipt.account,
            &receipt.device,
            payload.clone(),
        );
        let Value::Array(expected) = expected else {
            return Err(Error::Invalid);
        };
        if fields != expected[..9] {
            return Err(Error::Unauthorized);
        }
        Ok(payload)
    }
    pub fn reserve_source(
        &mut self,
        event_id: &str,
        source_id: &str,
        kind: SourceKind,
        now: u64,
    ) -> Result<SourceLease> {
        self.reserve_source_inner(event_id, source_id, kind, None, now)
    }
    /// Native cryptographic reservation, not Voice permission or RTC capability.
    /// Actual Voice producer must own a separate per-Voice MLS group and current
    /// native routing/member lease. A different channel is denied before mutation.
    pub fn reserve_bound_voice_source(
        &mut self,
        event_id: &str,
        source_id: &str,
        claim: &SourceTransportClaim,
        now: u64,
    ) -> Result<SourceLease> {
        if claim.voice_channel() != self.binding.channel {
            return Err(Error::Unauthorized);
        }
        self.reserve_source_inner(event_id, source_id, claim.codec(), Some(claim), now)
    }
    fn reserve_source_inner(
        &mut self,
        event_id: &str,
        source_id: &str,
        kind: SourceKind,
        claim: Option<&SourceTransportClaim>,
        now: u64,
    ) -> Result<SourceLease> {
        uuid(event_id)?;
        uuid(source_id)?;
        let (epoch, generation, sender, account, device) = self.current(now)?;
        if epoch > u32::MAX as u64 || sender > u16::MAX as u32 {
            return Err(Error::Limit);
        }
        let result = (|| {
            let tx = self
                .core
                .connection
                .transaction_with_behavior(TransactionBehavior::Immediate)
                .map_err(|_| Error::Database)?;
            check_revision(&tx, self.core.revision)?;
            let next: i64 = tx
                .query_row(
                    "SELECT COALESCE(MAX(context),0)+1 FROM sdk_sources WHERE epoch=? AND sender=?",
                    params![epoch.to_be_bytes().as_slice(), sender],
                    |r| r.get(0),
                )
                .map_err(|_| Error::Database)?;
            let context = u16::try_from(next).map_err(|_| Error::Limit)?;
            if context == 0 {
                return Err(Error::Limit);
            }
            if tx
                .execute(
                    "INSERT INTO sdk_sources VALUES(?,?,?,?)",
                    params![epoch.to_be_bytes().as_slice(), sender, context, source_id],
                )
                .map_err(|_| Error::Database)?
                != 1
            {
                return Err(Error::Database);
            }
            let revision = advance(&tx, self.core.revision, now)?;
            #[cfg(test)]
            source_fixture_exit("before-context-commit");
            tx.commit().map_err(|_| Error::Database)?;
            self.core.revision = revision;
            #[cfg(test)]
            source_fixture_exit("after-context-commit");
            let mut payload = vec![
                Value::Integer(epoch.into()),
                Value::Integer(sender.into()),
                Value::Integer(context.into()),
                Value::Text(source_id.into()),
                Value::Text(kind.name().into()),
            ];
            let domain = if let Some(claim) = claim {
                payload.extend(claim.payload_tail());
                VOICE_SOURCE_DOMAIN
            } else {
                SOURCE_DOMAIN
            };
            let inner =
                encode(&self.envelope(domain, event_id, &account, &device, Value::Array(payload)))?;
            let result = self.core.send_inner(
                event_id,
                &inner,
                now,
                |_| Ok(()),
                Some((&self.binding.channel, epoch, generation)),
            );
            let announcement = self.core.quarantine(result)?;
            #[cfg(test)]
            source_fixture_exit("after-announcement-commit");
            Ok(SourceLease {
                owner: self.core.owner,
                epoch,
                generation,
                sender,
                context,
                source: source_id.into(),
                kind,
                sending: true,
                announcement,
            })
        })();
        if result.is_err() {
            self.retire_native();
        }
        result
    }
    pub fn receive_source(&mut self, event_id: &str, wire: &[u8], now: u64) -> Result<SourceLease> {
        uuid(event_id)?;
        let (epoch, generation, _, _, _) = self.current(now)?;
        let result = (|| {
            let receipt = self.core.receive_inner(wire, now)?;
            let payload = self.checked_envelope(SOURCE_DOMAIN, event_id, &receipt)?;
            let Value::Array(fields) = payload else {
                return Err(Error::Invalid);
            };
            if fields.len() != 5 {
                return Err(Error::Invalid);
            }
            let ep = integer(&fields[0])?;
            let sender = u32::try_from(integer(&fields[1])?).map_err(|_| Error::Invalid)?;
            let context = u16::try_from(integer(&fields[2])?).map_err(|_| Error::Invalid)?;
            let Value::Text(source) = &fields[3] else {
                return Err(Error::Invalid);
            };
            uuid(source)?;
            let kind = match &fields[4] {
                Value::Text(s) if s == "opus" => SourceKind::Opus,
                Value::Text(s) if s == "vp8" => SourceKind::Vp8,
                _ => return Err(Error::Invalid),
            };
            if ep != epoch
                || ep > u32::MAX as u64
                || sender != receipt.sender
                || sender > u16::MAX as u32
                || context == 0
                || self.received.len() >= 4096
                || !self.received.insert((epoch, sender, context))
            {
                return Err(Error::Unauthorized);
            }
            Ok(SourceLease {
                owner: self.core.owner,
                epoch,
                generation,
                sender,
                context,
                source: source.clone(),
                kind,
                sending: false,
                announcement: Vec::new(),
            })
        })();
        if result.is_err() {
            self.retire_native();
        }
        result
    }
    /// Native-only consumption. Actual fixed worker/bridge delivery is unimplemented.
    pub(super) fn consume_for_native_worker(
        &mut self,
        lease: SourceLease,
        now: u64,
    ) -> Result<WorkerMaterial> {
        let (epoch, generation, _, _, _) = self.current(now)?;
        if lease.owner != self.core.owner
            || lease.epoch != epoch
            || lease.generation != generation
            || lease.source.is_empty()
        {
            return Err(Error::Stale);
        }
        if self.spent.len() >= 4096 || !self.spent.insert((epoch, lease.sender, lease.context)) {
            return Err(Error::Replay);
        }
        let committed = (|| {
            let tx = self
                .core
                .connection
                .transaction_with_behavior(TransactionBehavior::Immediate)
                .map_err(|_| Error::Database)?;
            check_revision(&tx, self.core.revision)?;
            let next = advance(&tx, self.core.revision, now)?;
            tx.commit().map_err(|_| Error::Database)?;
            self.core.revision = next;
            Ok(())
        })();
        if let Err(error) = committed {
            self.retire_native();
            return Err(error);
        }
        let exported = self
            .core
            .group
            .as_ref()
            .ok_or(Error::Quarantined)?
            .export_secret(&self.core.crypto, "SFrame 1.0 Base Key", b"", 32)
            .map_err(|_| Error::Provider)?;
        let kid = ((lease.context as u64) << 48) | ((lease.sender as u64) << 32) | epoch;
        Ok(WorkerMaterial {
            base: Zeroizing::new(exported),
            kid,
            kind: lease.kind,
            sending: lease.sending,
        })
    }
}
fn integer(v: &Value) -> Result<u64> {
    match v {
        Value::Integer(i) => u64::try_from(*i).map_err(|_| Error::Invalid),
        _ => Err(Error::Invalid),
    }
}
#[cfg(test)]
fn source_fixture_exit(point: &str) {
    if std::env::var("MNEMA_SDK_SOURCE_FAULT").as_deref() == Ok(point) {
        std::process::exit(73);
    }
}
fn encode(v: &Value) -> Result<Vec<u8>> {
    let mut bytes = Vec::new();
    coset::cbor::ser::into_writer(v, &mut bytes).map_err(|_| Error::Invalid)?;
    if bytes.len() > 32768 {
        Err(Error::Limit)
    } else {
        Ok(bytes)
    }
}
pub(super) fn uuid(v: &str) -> Result<()> {
    if v.len() != 36
        || v == "00000000-0000-0000-0000-000000000000"
        || !v.bytes().enumerate().all(|(i, b)| {
            if [8, 13, 18, 23].contains(&i) {
                b == b'-'
            } else {
                b.is_ascii_digit() || (b'a'..=b'f').contains(&b)
            }
        })
    {
        Err(Error::Invalid)
    } else {
        Ok(())
    }
}
/// Flat profile scanner: at most ten-element outer plus one nested source5/voice11.
/// Reject nesting/size bombs before recursive library parsing.
pub(super) fn scan(wire: &[u8]) -> Result<()> {
    if wire.is_empty() || wire.len() > 32768 {
        return Err(Error::Invalid);
    }
    let mut pos = 0;
    let mut nodes = 0;
    let mut pending = vec![(1u64, 0usize)];
    while let Some((mut count, depth)) = pending.pop() {
        while count > 0 {
            count -= 1;
            nodes += 1;
            if nodes > 32 || pos >= wire.len() {
                return Err(Error::Invalid);
            }
            let head = wire[pos];
            pos += 1;
            let major = head >> 5;
            let additional = head & 31;
            let width = match additional {
                0..=23 => 0,
                24 => 1,
                25 => 2,
                26 => 4,
                27 => 8,
                _ => return Err(Error::Invalid),
            };
            if pos + width > wire.len() {
                return Err(Error::Invalid);
            }
            let n = if width == 0 {
                additional as u64
            } else {
                let mut n = 0u64;
                for b in &wire[pos..pos + width] {
                    n = (n << 8) | (*b as u64);
                }
                pos += width;
                let min = match width {
                    1 => 24,
                    2 => 256,
                    4 => 65536,
                    8 => 4294967296,
                    _ => return Err(Error::Invalid),
                };
                if n < min {
                    return Err(Error::Invalid);
                }
                n
            };
            match major {
                0 => {}
                2 | 3 => {
                    let n = usize::try_from(n).map_err(|_| Error::Invalid)?;
                    pos = pos.checked_add(n).ok_or(Error::Invalid)?;
                    if pos > wire.len() {
                        return Err(Error::Invalid);
                    }
                }
                4 => {
                    if depth >= 2 || n > if depth == 0 { 10 } else { 11 } {
                        return Err(Error::Invalid);
                    }
                    pending.push((count, depth));
                    pending.push((n, depth + 1));
                    break;
                }
                _ => return Err(Error::Invalid),
            }
        }
    }
    if pos != wire.len() {
        return Err(Error::Invalid);
    }
    Ok(())
}

impl Sdk {
    pub(super) fn owner_facts_inner(&self, now: u64) -> Result<crate::host::NativeOwnerFacts> {
        let (epoch, generation, _, account, device) = self.current(now)?;
        let group = self.core.group.as_ref().ok_or(Error::WrongPhase)?;
        let own = group.own_leaf().ok_or(Error::Trust)?;
        Ok(crate::host::NativeOwnerFacts {
            origin: self.core.pin.scope.origin().into(),
            community: self.core.pin.scope.community().into(),
            channel: self.binding.channel.clone(),
            group: group.group_id().as_slice().into(),
            account,
            device,
            identity: own.credential().serialized_content().into(),
            signature_key: own.signature_key().as_slice().into(),
            epoch,
            generation,
        })
    }
}

impl Sdk {
    /// Root-approved actual validated KeyPackage only. Native caller must also
    /// hold actual admin/channel and OOB device approval; signed roster alone
    /// does not authenticate a bearer account session. Membership advances burn
    /// existing native frame keys/handles and never reuse their source contexts.
    pub fn add_root_approved_native_peer(
        &mut self,
        commit_event: &str,
        welcome_event: &str,
        key_package: &[u8],
        now: u64,
    ) -> Result<crate::HostAdmission> {
        self.current(now)?;
        self.bridges.clear();
        let result =
            self.core
                .add_root_approved_native_peer(commit_event, welcome_event, key_package, now);
        if result.is_err() {
            self.retire_native()
        }
        result
    }
}

impl Sdk {
    pub fn pending_native_host_publication(
        &self,
        event_id: &str,
        now: u64,
    ) -> Result<Option<Vec<u8>>> {
        uuid(event_id)?;
        let (epoch, generation, _, _, _) = self.current(now)?;
        let matched:bool=self.core.connection.query_row("SELECT EXISTS(SELECT 1 FROM sdk_events WHERE event=? AND channel=? AND epoch=? AND generation=?)",params![event_id,self.binding.channel,epoch.to_be_bytes().as_slice(),generation.to_be_bytes().as_slice()],|r|r.get(0)).map_err(|_|Error::Database)?;
        if !matched {
            return Err(Error::Stale);
        }
        self.core.connection.query_row("SELECT wire FROM core_outbox WHERE event_id=? AND kind IN ('host_commit','host_welcome')",[event_id],|r|r.get(0)).optional().map_err(|_|Error::Database)
    }
}

#[path = "fresh_join.rs"]
mod fresh_join;

#[path = "dispatch.rs"]
mod dispatch;
pub use dispatch::{ProtectedReceived, VerifiedSource, VerifiedSourceFacts};

#[path = "voice_binding.rs"]
mod voice_binding;
pub use voice_binding::{ProtectedSourceBinding, SourcePurpose, SourceTransportClaim};

#[path = "chat_domain.rs"]
mod chat_domain;
#[path = "chat_event_projection.rs"]
mod chat_event_projection;
pub use chat_domain::{
    CHAT_EVENT_DOMAIN, CHAT_EVENT_VERSION, ChatEventClaim, ChatKind, ChatOperation, ReactionAction,
    VerifiedChatEvent,
};
pub use chat_event_projection::{NativeProtectedEventScope, NativeReservedChatEvent};
impl Sdk {
    /// Shape-checked typed event only. Native owner must authorize mutation from
    /// actual verified history/current role BEFORE calling; no permission comes
    /// from this producer claim. Metadata remains actual delivery evidence.
    pub fn send_chat_event(
        &mut self,
        event: &str,
        claim: &ChatEventClaim,
        now: u64,
    ) -> Result<Vec<u8>> {
        uuid(event)?;
        let (epoch, generation, _, account, device) = self.current(now)?;
        let payload = claim.payload();
        // Same parser enforces logical create/ref event identity before mutation.
        let native_scope = self.native_protected_event_scope(now)?;
        VerifiedChatEvent::from_authenticated_payload(
            native_scope,
            event,
            &account,
            &device,
            &payload,
        )?;
        let inner = encode(&self.envelope(CHAT_EVENT_DOMAIN, event, &account, &device, payload))?;
        let result = self.core.send_inner(
            event,
            &inner,
            now,
            |_| Ok(()),
            Some((&self.binding.channel, epoch, generation)),
        );
        let result = self.core.quarantine(result);
        if result.is_err() {
            self.retire_native()
        }
        result
    }
}

#[path = "community.rs"]
mod community;
pub use community::{NativeCommunityAnchor, VerifiedCommunityAuthorization, VerifiedVoiceCreation};
