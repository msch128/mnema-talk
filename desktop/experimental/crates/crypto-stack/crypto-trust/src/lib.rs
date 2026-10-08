//! Ignored evaluation only. Native pin/bootstrap, durable floors and issuer are external.
use coset::{CoseSign1, Header, TaggedCborSerializable, cbor::value::Value};
use ed25519_dalek::{Signature, VerifyingKey};
use mnema_crypto_adapter_candidate::{
    Adapter, AuthorizedStageHandle, GroupHandle, GroupSummary, NativeApprovalRecord,
    NativeTrustPolicy, Scope, StageHandle, StageInspection,
};
use openmls::prelude::MlsGroup;
use openmls_rust_crypto::OpenMlsRustCrypto;
use std::collections::{HashMap, HashSet};
use std::sync::atomic::{AtomicU64, Ordering};

const DOMAIN: &str = "MnemaTalk DeviceAuthorization";
const AAD: &[u8] = b"MnemaTalk DeviceAuthorization/v1";
const PROTECTED: &[u8] = &[0xa1, 0x01, 0x32]; // {1: -19}, fully specified Ed25519
const MAX_WIRE: usize = 128 * 1024;
const MAX_NODES: usize = 4096;
const MAX_GROUPS: usize = 32;
const MAX_DEVICES: usize = 128;
const MAX_LIFETIME: u64 = 24 * 60 * 60;
static NEXT_OWNER: AtomicU64 = AtomicU64::new(1);

pub type Result<T> = std::result::Result<T, Error>;
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    InvalidPin,
    InvalidWire,
    UnsupportedProfile,
    BadSignature,
    WrongScope,
    UnknownGroup,
    InvalidRecord,
    ConflictingRecord,
    TimeInvalid,
    ClockRollback,
    Expired,
    Replay,
    StaleHandle,
    UnapprovedDevice,
    Limit,
    Adapter(mnema_crypto_adapter_candidate::Error),
}
impl From<mnema_crypto_adapter_candidate::Error> for Error {
    fn from(error: mnema_crypto_adapter_candidate::Error) -> Self {
        Self::Adapter(error)
    }
}

/// Only a native store/bootstrap may supply floors. Constructor validates shape,
/// not freshness or durable rollback resistance. Never deserialize server floors.
pub struct NativeGroupFloor {
    group: Vec<u8>,
    generation: u64,
}
impl NativeGroupFloor {
    pub fn from_native_store(group: &[u8], generation: u64) -> Result<Self> {
        if group.is_empty() || group.len() > 128 {
            return Err(Error::InvalidPin);
        }
        Ok(Self {
            group: group.to_vec(),
            generation,
        })
    }
}
#[derive(Clone)]
struct Device {
    account: String,
    device: String,
    identity: Vec<u8>,
    key: [u8; 32],
}
struct Roster {
    generation: u64,
    expires: u64,
    devices: Vec<Device>,
}
struct GroupState {
    floor: u64,
    roster: Option<Roster>,
}
/// Opaque verified result: no public constructor, fields or Deserialize.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct VerifiedRoster {
    owner: u64,
    group: Vec<u8>,
    generation: u64,
}
/// Opaque exact device approval; every use must recheck its owning verifier.
#[derive(Clone)]
pub struct VerifiedDevice {
    roster: VerifiedRoster,
    device: Device,
}
impl VerifiedDevice {
    pub fn account(&self) -> &str {
        &self.device.account
    }
    pub fn device(&self) -> &str {
        &self.device.device
    }
}

pub struct TrustVerifier {
    scope: Scope,
    authority: VerifyingKey,
    groups: HashMap<Vec<u8>, GroupState>,
    owner: u64,
    last_time: u64,
}
impl TrustVerifier {
    /// Root key and floors must already be approved independently in native code.
    /// No root fetch, replacement, server boolean or signature-key discovery exists.
    pub fn from_native_pin(
        scope: Scope,
        authority_key: &[u8],
        floors: Vec<NativeGroupFloor>,
        now: u64,
    ) -> Result<Self> {
        let bytes: [u8; 32] = authority_key.try_into().map_err(|_| Error::InvalidPin)?;
        let authority = VerifyingKey::from_bytes(&bytes).map_err(|_| Error::InvalidPin)?;
        if authority.is_weak() || floors.is_empty() || floors.len() > MAX_GROUPS {
            return Err(Error::InvalidPin);
        }
        let mut groups = HashMap::new();
        for floor in floors {
            if groups
                .insert(
                    floor.group,
                    GroupState {
                        floor: floor.generation,
                        roster: None,
                    },
                )
                .is_some()
            {
                return Err(Error::InvalidPin);
            }
        }
        let owner = NEXT_OWNER
            .try_update(Ordering::Relaxed, Ordering::Relaxed, |n| n.checked_add(1))
            .map_err(|_| Error::Limit)?;
        Ok(Self {
            scope,
            authority,
            groups,
            owner,
            last_time: now,
        })
    }
    fn tick(&mut self, now: u64) -> Result<()> {
        if now < self.last_time {
            return Err(Error::ClockRollback);
        }
        self.last_time = now;
        Ok(())
    }
    pub fn verify_roster(&mut self, wire: &[u8], now: u64) -> Result<VerifiedRoster> {
        self.tick(now)?;
        let envelope = decode_bounded(wire)?;
        let Value::Tag(18, body) = &envelope else {
            return Err(Error::UnsupportedProfile);
        };
        let fields = array(body, 4)?;
        if bytes(&fields[0])? != PROTECTED
            || !matches!(&fields[1], Value::Map(map) if map.is_empty())
            || bytes(&fields[3])?.len() != 64
        {
            return Err(Error::UnsupportedProfile);
        }
        let payload = bytes(&fields[2])?;
        // Validate fixed protected header before coset parses its nested bstr.
        let signed = CoseSign1::from_tagged_slice(wire).map_err(|_| Error::InvalidWire)?;
        if signed.unprotected != Header::default() {
            return Err(Error::UnsupportedProfile);
        }
        signed.verify_signature(AAD, |signature, transcript| {
            let signature = Signature::from_slice(signature).map_err(|_| Error::BadSignature)?;
            self.authority
                .verify_strict(transcript, &signature)
                .map_err(|_| Error::BadSignature)
        })?;
        let decoded = decode_bounded(payload)?;
        let claims = array(&decoded, 9)?;
        if text(&claims[0])? != DOMAIN || integer(&claims[1])? != 1 {
            return Err(Error::UnsupportedProfile);
        }
        if text(&claims[2])? != self.scope.origin() || text(&claims[3])? != self.scope.community() {
            return Err(Error::WrongScope);
        }
        let group = bytes(&claims[4])?;
        let state = self.groups.get(group).ok_or(Error::UnknownGroup)?;
        let generation = integer(&claims[5])?;
        let issued = integer(&claims[6])?;
        let expires = integer(&claims[7])?;
        if issued > now || expires <= issued || expires - issued > MAX_LIFETIME {
            return Err(Error::TimeInvalid);
        }
        if now >= expires {
            return Err(Error::Expired);
        }
        if generation == 0 || generation <= state.floor {
            return Err(Error::Replay);
        }
        let Value::Array(entries) = &claims[8] else {
            return Err(Error::InvalidRecord);
        };
        if entries.len() > MAX_DEVICES {
            return Err(Error::Limit);
        }
        let mut devices = Vec::with_capacity(entries.len());
        let mut identities = HashSet::new();
        let mut keys = HashSet::new();
        let mut previous: Option<(String, String)> = None;
        for entry in entries {
            let fields = array(entry, 4)?;
            let account = text(&fields[0])?;
            let device = text(&fields[1])?;
            let identity = bytes(&fields[2])?;
            let key: [u8; 32] = bytes(&fields[3])?
                .try_into()
                .map_err(|_| Error::InvalidRecord)?;
            if !valid_id(account)
                || !valid_id(device)
                || identity.is_empty()
                || identity.len() > 256
            {
                return Err(Error::InvalidRecord);
            }
            let name = (account.to_owned(), device.to_owned());
            if previous.as_ref().is_some_and(|old| old >= &name)
                || !identities.insert(identity.to_vec())
                || !keys.insert(key)
            {
                return Err(Error::ConflictingRecord);
            }
            let device_key = VerifyingKey::from_bytes(&key).map_err(|_| Error::InvalidRecord)?;
            if device_key.is_weak() {
                return Err(Error::InvalidRecord);
            }
            previous = Some(name);
            devices.push(Device {
                account: account.to_owned(),
                device: device.to_owned(),
                identity: identity.to_vec(),
                key,
            });
        }
        let group = group.to_vec();
        let state = self.groups.get_mut(&group).ok_or(Error::UnknownGroup)?;
        // Publish only after complete signature/scope/time/record validation.
        state.floor = generation;
        state.roster = Some(Roster {
            generation,
            expires,
            devices,
        });
        Ok(VerifiedRoster {
            owner: self.owner,
            group,
            generation,
        })
    }
    fn current(&mut self, handle: &VerifiedRoster, group: &[u8], now: u64) -> Result<&Roster> {
        self.tick(now)?;
        if handle.owner != self.owner || handle.group != group {
            return Err(Error::StaleHandle);
        }
        let roster = self
            .groups
            .get(group)
            .and_then(|state| state.roster.as_ref())
            .ok_or(Error::StaleHandle)?;
        if roster.generation != handle.generation {
            return Err(Error::StaleHandle);
        }
        if now >= roster.expires {
            return Err(Error::Expired);
        }
        Ok(roster)
    }
    pub fn verify_device(
        &mut self,
        handle: &VerifiedRoster,
        group: &[u8],
        identity: &[u8],
        key: &[u8],
        now: u64,
    ) -> Result<VerifiedDevice> {
        let device = self
            .current(handle, group, now)?
            .devices
            .iter()
            .find(|device| device.identity == identity && device.key.as_slice() == key)
            .ok_or(Error::UnapprovedDevice)?
            .clone();
        Ok(VerifiedDevice {
            roster: handle.clone(),
            device,
        })
    }
    pub fn validate_device(
        &mut self,
        proof: &VerifiedDevice,
        group: &[u8],
        identity: &[u8],
        key: &[u8],
        now: u64,
    ) -> Result<()> {
        let current = self.verify_device(&proof.roster, group, identity, key, now)?;
        if current.device.account != proof.device.account
            || current.device.device != proof.device.device
            || current.device.identity != proof.device.identity
            || current.device.key != proof.device.key
        {
            return Err(Error::UnapprovedDevice);
        }
        Ok(())
    }
    /// Native MLS state transfer only; does not prove Welcome/bootstrap freshness.
    /// Returned adapter retains no public escape to community-wide unchecked policy.
    pub fn adopt_adapter(
        &mut self,
        handle: &VerifiedRoster,
        provider: OpenMlsRustCrypto,
        group: MlsGroup,
        now: u64,
    ) -> Result<GroupBoundAdapter> {
        let group_id = group.group_id().as_slice().to_vec();
        let roster = self.current(handle, &group_id, now)?;
        if roster.devices.is_empty() {
            return Err(Error::UnapprovedDevice);
        }
        let mut records = Vec::with_capacity(roster.devices.len());
        for device in &roster.devices {
            records.push(NativeApprovalRecord::from_native_store(
                &device.account,
                &device.device,
                &device.identity,
                &device.key,
            )?);
        }
        let policy = NativeTrustPolicy::from_native_store(self.scope.clone(), records)?;
        let mut adapter = Adapter::from_native_state(self.scope.clone(), policy, provider)?;
        let group = adapter.adopt_native_group(group)?;
        Ok(GroupBoundAdapter {
            adapter,
            group,
            authorization: handle.clone(),
            group_id,
        })
    }
}

/// One group, one verified roster, underlying adapter private. Every public
/// operation rechecks the original verifier; replacement/expiry stops access.
pub struct GroupBoundAdapter {
    adapter: Adapter,
    group: GroupHandle,
    authorization: VerifiedRoster,
    group_id: Vec<u8>,
}
impl GroupBoundAdapter {
    fn guard(&self, verifier: &mut TrustVerifier, now: u64) -> Result<()> {
        verifier
            .current(&self.authorization, &self.group_id, now)
            .map(|_| ())
    }
    pub fn summary(&self, verifier: &mut TrustVerifier, now: u64) -> Result<GroupSummary> {
        self.guard(verifier, now)?;
        Ok(self.adapter.summary(self.group)?)
    }
    pub fn stage_commit(
        &mut self,
        verifier: &mut TrustVerifier,
        wire: &[u8],
        now: u64,
    ) -> Result<StageHandle> {
        self.guard(verifier, now)?;
        Ok(self.adapter.stage_commit(self.group, wire)?)
    }
    pub fn inspect_stage(
        &mut self,
        verifier: &mut TrustVerifier,
        stage: StageHandle,
        now: u64,
    ) -> Result<StageInspection> {
        self.guard(verifier, now)?;
        Ok(self.adapter.inspect_stage(stage)?)
    }
    pub fn authorize_stage(
        &mut self,
        verifier: &mut TrustVerifier,
        stage: StageHandle,
        now: u64,
    ) -> Result<AuthorizedStageHandle> {
        self.guard(verifier, now)?;
        Ok(self.adapter.authorize_stage(stage)?)
    }
    pub fn merge_authorized(
        &mut self,
        verifier: &mut TrustVerifier,
        stage: AuthorizedStageHandle,
        now: u64,
    ) -> Result<GroupSummary> {
        self.guard(verifier, now)?;
        Ok(self.adapter.merge_authorized(stage)?)
    }
    pub fn reject_stage(
        &mut self,
        verifier: &mut TrustVerifier,
        stage: StageHandle,
        now: u64,
    ) -> Result<()> {
        self.guard(verifier, now)?;
        Ok(self.adapter.reject_stage(stage)?)
    }
    pub fn receive_application(
        &mut self,
        verifier: &mut TrustVerifier,
        wire: &[u8],
        now: u64,
    ) -> Result<Vec<u8>> {
        self.guard(verifier, now)?;
        Ok(self.adapter.receive_application(self.group, wire)?)
    }
}

fn valid_id(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 128
        && value
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
}
fn bytes(value: &Value) -> Result<&[u8]> {
    match value {
        Value::Bytes(bytes) => Ok(bytes),
        _ => Err(Error::InvalidWire),
    }
}
fn text(value: &Value) -> Result<&str> {
    match value {
        Value::Text(text) => Ok(text),
        _ => Err(Error::InvalidWire),
    }
}
fn integer(value: &Value) -> Result<u64> {
    match value {
        Value::Integer(integer) => (*integer).try_into().map_err(|_| Error::InvalidWire),
        _ => Err(Error::InvalidWire),
    }
}
fn array(value: &Value, size: usize) -> Result<&[Value]> {
    match value {
        Value::Array(array) if array.len() == size => Ok(array),
        _ => Err(Error::InvalidWire),
    }
}
fn encode(value: &Value) -> Result<Vec<u8>> {
    let mut encoded = Vec::new();
    coset::cbor::ser::into_writer(value, &mut encoded).map_err(|_| Error::InvalidWire)?;
    Ok(encoded)
}
fn decode_bounded(wire: &[u8]) -> Result<Value> {
    preflight(wire)?;
    let decoded: Value = coset::cbor::de::from_reader(wire).map_err(|_| Error::InvalidWire)?;
    if encode(&decoded)? != wire {
        return Err(Error::InvalidWire);
    }
    Ok(decoded)
}
// Structural budget scanner only; crypto and COSE are handled by libraries.
// Iterate before recursive CBOR parsing, including protected/payload separately.
fn preflight(wire: &[u8]) -> Result<()> {
    if wire.is_empty() || wire.len() > MAX_WIRE {
        return Err(Error::Limit);
    }
    let mut index = 0usize;
    let mut nodes = 0usize;
    let mut stack = vec![(1u64, 0usize)];
    while let Some((remaining, depth)) = stack.pop() {
        if remaining == 0 {
            continue;
        }
        if depth > 8 || nodes >= MAX_NODES || remaining > MAX_NODES as u64 {
            return Err(Error::Limit);
        }
        stack.push((remaining - 1, depth));
        nodes += 1;
        let byte = *wire.get(index).ok_or(Error::InvalidWire)?;
        index += 1;
        let major = byte >> 5;
        let additional = byte & 31;
        let argument = match additional {
            0..=23 => u64::from(additional),
            24..=27 => {
                let width = 1usize << (additional - 24);
                let end = index.checked_add(width).ok_or(Error::Limit)?;
                let bytes = wire.get(index..end).ok_or(Error::InvalidWire)?;
                let mut value = 0u64;
                for byte in bytes {
                    value = (value << 8) | u64::from(*byte);
                }
                let minimum = match width {
                    1 => 24,
                    2 => 256,
                    4 => 65536,
                    _ => 1u64 << 32,
                };
                if value < minimum {
                    return Err(Error::InvalidWire);
                }
                index = end;
                value
            }
            _ => return Err(Error::InvalidWire),
        };
        match major {
            0 | 1 => (),
            2 | 3 => {
                let size = usize::try_from(argument).map_err(|_| Error::Limit)?;
                index = index.checked_add(size).ok_or(Error::Limit)?;
                if index > wire.len() {
                    return Err(Error::InvalidWire);
                }
            }
            4 => stack.push((argument, depth + 1)),
            5 => stack.push((argument.checked_mul(2).ok_or(Error::Limit)?, depth + 1)),
            6 => stack.push((1, depth + 1)),
            _ => return Err(Error::InvalidWire),
        }
    }
    if index != wire.len() {
        return Err(Error::InvalidWire);
    }
    Ok(())
}

#[cfg(test)]
mod tests;
