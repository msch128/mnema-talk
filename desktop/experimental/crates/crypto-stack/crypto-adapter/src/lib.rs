//! Ignored qualification candidate; no networking, persistence, UI IPC or device bootstrap.
//! Native trust records MUST already be verified independently by the caller.
use openmls::prelude::{tls_codec::Deserialize, *};
use openmls_rust_crypto::OpenMlsRustCrypto;
use std::collections::{HashMap, HashSet};
use std::sync::atomic::{AtomicU64, Ordering};

const MAX_WIRE: usize = 64 * 1024;
const MAX_GROUPS: usize = 32;
const MAX_DEVICES: usize = 128;
const SUITE: Ciphersuite = Ciphersuite::MLS_128_DHKEMX25519_CHACHA20POLY1305_SHA256_Ed25519;
static NEXT_ADAPTER: AtomicU64 = AtomicU64::new(1);

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    InvalidScope,
    InvalidTrustRecord,
    ConflictingTrustRecord,
    ScopeMismatch,
    Limit,
    StaleHandle,
    Busy,
    InvalidWire,
    ProtocolRejected,
    UnexpectedMessage,
    UnapprovedDevice,
    IdentityChange,
    UnsupportedPolicyChange,
    NotInspected,
    NotAuthorized,
    Removed,
    Retired,
    ProviderFailure,
}

#[cfg(test)]
mod tests;
pub type Result<T> = std::result::Result<T, Error>;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Scope {
    origin: String,
    community: String,
}
impl Scope {
    pub fn new(origin: &str, community: &str) -> Result<Self> {
        if origin.len() > 2048
            || origin.contains('@')
            || origin
                .chars()
                .any(|c| c.is_control() || c.is_whitespace() || c == '\\')
            || !valid_id(community)
        {
            return Err(Error::InvalidScope);
        }
        let parsed = url::Url::parse(origin).map_err(|_| Error::InvalidScope)?;
        if parsed.scheme() != "https"
            || parsed.host_str().is_none()
            || !parsed.username().is_empty()
            || parsed.password().is_some()
            || parsed.query().is_some()
            || parsed.fragment().is_some()
            || parsed.path() != "/"
        {
            return Err(Error::InvalidScope);
        }
        Ok(Self {
            origin: parsed.origin().ascii_serialization(),
            community: community.to_owned(),
        })
    }
    pub fn origin(&self) -> &str {
        &self.origin
    }
    pub fn community(&self) -> &str {
        &self.community
    }
}
fn valid_id(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 128
        && value
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
}

/// Data copied from an independently verified native store. This constructor
/// validates shape only: it does NOT verify device approval or authenticate users.
/// No Deserialize/IPC conversion exists in this candidate.
#[derive(Clone)]
pub struct NativeApprovalRecord {
    account: String,
    device: String,
    identity: Vec<u8>,
    key: [u8; 32],
}
impl NativeApprovalRecord {
    pub fn from_native_store(
        account: &str,
        device: &str,
        identity: &[u8],
        key: &[u8],
    ) -> Result<Self> {
        if !valid_id(account) || !valid_id(device) || identity.is_empty() || identity.len() > 256 {
            return Err(Error::InvalidTrustRecord);
        }
        let key = key.try_into().map_err(|_| Error::InvalidTrustRecord)?;
        Ok(Self {
            account: account.to_owned(),
            device: device.to_owned(),
            identity: identity.to_vec(),
            key,
        })
    }
}
pub struct NativeTrustPolicy {
    scope: Scope,
    devices: Vec<NativeApprovalRecord>,
}
impl NativeTrustPolicy {
    pub fn from_native_store(scope: Scope, devices: Vec<NativeApprovalRecord>) -> Result<Self> {
        if devices.is_empty() || devices.len() > MAX_DEVICES {
            return Err(Error::Limit);
        }
        let mut identities = HashSet::new();
        let mut keys = HashSet::new();
        let mut names = HashSet::new();
        for device in &devices {
            if !identities.insert(device.identity.clone())
                || !keys.insert(device.key)
                || !names.insert((device.account.clone(), device.device.clone()))
            {
                return Err(Error::ConflictingTrustRecord);
            }
        }
        Ok(Self { scope, devices })
    }
    fn approval(&self, identity: &[u8], key: &[u8]) -> Result<&NativeApprovalRecord> {
        self.devices
            .iter()
            .find(|d| d.identity == identity && d.key.as_slice() == key)
            .ok_or(Error::UnapprovedDevice)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub struct GroupHandle {
    owner: u64,
    generation: u64,
    serial: u64,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub struct StageHandle {
    owner: u64,
    generation: u64,
    serial: u64,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct AuthorizedStageHandle {
    stage: StageHandle,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct DeviceCandidate {
    pub identity: Vec<u8>,
    pub public_key: [u8; 32],
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct StageInspection {
    pub scope: Scope,
    pub group: GroupHandle,
    pub from_epoch: u64,
    pub to_epoch: u64,
    pub additions: Vec<DeviceCandidate>,
    pub updates: Vec<DeviceCandidate>,
    pub removals: Vec<u32>,
    pub update_path: Option<DeviceCandidate>,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct GroupSummary {
    pub epoch: u64,
    pub members: usize,
    pub active: bool,
}
struct PendingStage {
    group: GroupHandle,
    epoch: u64,
    commit: StagedCommit,
    inspection: StageInspection,
    sender_identity: Vec<u8>,
    inspected: bool,
    authorized: bool,
}

pub struct Adapter {
    scope: Scope,
    policy: NativeTrustPolicy,
    provider: OpenMlsRustCrypto,
    owner: u64,
    retired: bool,
    generation: u64,
    next_serial: u64,
    groups: HashMap<GroupHandle, MlsGroup>,
    stages: HashMap<StageHandle, PendingStage>,
    quarantined: HashSet<GroupHandle>,
}
impl Adapter {
    /// Native ownership transfer only. Group creation/Welcome admission and
    /// trust bootstrap are separate, unimplemented application responsibilities.
    pub fn from_native_state(
        scope: Scope,
        policy: NativeTrustPolicy,
        provider: OpenMlsRustCrypto,
    ) -> Result<Self> {
        if scope != policy.scope {
            return Err(Error::ScopeMismatch);
        }
        let owner = NEXT_ADAPTER
            .try_update(Ordering::Relaxed, Ordering::Relaxed, |id| id.checked_add(1))
            .map_err(|_| Error::Limit)?;
        Ok(Self {
            scope,
            policy,
            provider,
            owner,
            retired: false,
            generation: 1,
            next_serial: 1,
            groups: HashMap::new(),
            stages: HashMap::new(),
            quarantined: HashSet::new(),
        })
    }
    fn serial(&mut self) -> Result<u64> {
        let next = self.next_serial.checked_add(1).ok_or(Error::Limit)?;
        let serial = self.next_serial;
        self.next_serial = next;
        Ok(serial)
    }
    pub fn adopt_native_group(&mut self, group: MlsGroup) -> Result<GroupHandle> {
        if self.retired {
            return Err(Error::Retired);
        }
        if self.groups.len() >= MAX_GROUPS || group.members().count() > MAX_DEVICES {
            return Err(Error::Limit);
        }
        if self
            .groups
            .values()
            .any(|existing| existing.group_id() == group.group_id())
        {
            return Err(Error::Busy);
        }
        if group.ciphersuite() != SUITE {
            return Err(Error::UnsupportedPolicyChange);
        }
        if !group.is_active() {
            return Err(Error::Removed);
        }
        for member in group.members() {
            if member.credential.credential_type() != CredentialType::Basic {
                return Err(Error::UnsupportedPolicyChange);
            }
            self.policy.approval(
                member.credential.serialized_content(),
                &member.signature_key,
            )?;
        }
        let handle = GroupHandle {
            owner: self.owner,
            generation: self.generation,
            serial: self.serial()?,
        };
        self.groups.insert(handle, group);
        Ok(handle)
    }
    fn group(&self, handle: GroupHandle) -> Result<&MlsGroup> {
        if self.retired {
            return Err(Error::Retired);
        }
        if handle.owner != self.owner || handle.generation != self.generation {
            return Err(Error::StaleHandle);
        }
        if self.quarantined.contains(&handle) {
            return Err(Error::ProviderFailure);
        }
        self.groups.get(&handle).ok_or(Error::StaleHandle)
    }
    pub fn summary(&self, handle: GroupHandle) -> Result<GroupSummary> {
        let group = self.group(handle)?;
        Ok(GroupSummary {
            epoch: group.epoch().as_u64(),
            members: group.members().count(),
            active: group.is_active(),
        })
    }
    fn parse(wire: &[u8]) -> Result<ProtocolMessage> {
        if wire.is_empty() || wire.len() > MAX_WIRE {
            return Err(Error::InvalidWire);
        }
        MlsMessageIn::tls_deserialize_exact(wire)
            .map_err(|_| Error::InvalidWire)?
            .try_into_protocol_message()
            .map_err(|_| Error::UnexpectedMessage)
    }
    fn receive(
        &mut self,
        handle: GroupHandle,
        wire: &[u8],
        expected: ContentType,
    ) -> Result<(ProcessedMessage, Vec<u8>)> {
        let message = Self::parse(wire)?;
        // Routing checks precede the provider's stateful receive ratchet. A
        // caller using the wrong API must be able to retry the correct API.
        if message.content_type() != expected {
            return Err(Error::UnexpectedMessage);
        }
        let group = self.group(handle)?;
        if !group.is_active() {
            return Err(Error::Removed);
        }
        if message.group_id() != group.group_id() {
            return Err(Error::ProtocolRejected);
        }
        let epoch = group.epoch();
        if message.epoch() != epoch {
            return Err(Error::ProtocolRejected);
        }
        let group = self.groups.get_mut(&handle).ok_or(Error::StaleHandle)?;
        let processed = group
            .process_message(&self.provider, message)
            .map_err(|_| Error::ProtocolRejected)?;
        let Sender::Member(sender) = processed.sender() else {
            return Err(Error::UnsupportedPolicyChange);
        };
        let member = group
            .members()
            .find(|member| member.index == *sender)
            .ok_or(Error::ProtocolRejected)?;
        self.policy.approval(
            member.credential.serialized_content(),
            &member.signature_key,
        )?;
        if processed.credential().serialized_content() != member.credential.serialized_content() {
            return Err(Error::ProtocolRejected);
        }
        Ok((processed, member.credential.serialized_content().to_vec()))
    }
    pub fn stage_commit(&mut self, handle: GroupHandle, wire: &[u8]) -> Result<StageHandle> {
        self.group(handle)?;
        if self.stages.values().any(|stage| stage.group == handle) {
            return Err(Error::Busy);
        }
        let (processed, sender_identity) = self.receive(handle, wire, ContentType::Commit)?;
        let ProcessedMessageContent::StagedCommitMessage(commit) = processed.into_content() else {
            return Err(Error::UnexpectedMessage);
        };
        let group = self.group(handle)?;
        let candidate = |leaf: &LeafNode| -> Result<DeviceCandidate> {
            if leaf.credential().credential_type() != CredentialType::Basic
                || leaf.credential().serialized_content().len() > 256
            {
                return Err(Error::UnsupportedPolicyChange);
            }
            Ok(DeviceCandidate {
                identity: leaf.credential().serialized_content().to_vec(),
                public_key: leaf
                    .signature_key()
                    .as_slice()
                    .try_into()
                    .map_err(|_| Error::UnsupportedPolicyChange)?,
            })
        };
        let inspection = StageInspection {
            scope: self.scope.clone(),
            group: handle,
            from_epoch: group.epoch().as_u64(),
            to_epoch: commit.epoch().as_u64(),
            additions: commit
                .add_proposals()
                .map(|p| candidate(p.add_proposal().key_package().leaf_node()))
                .collect::<Result<_>>()?,
            updates: commit
                .update_proposals()
                .map(|p| candidate(p.update_proposal().leaf_node()))
                .collect::<Result<_>>()?,
            removals: commit
                .remove_proposals()
                .map(|p| p.remove_proposal().removed().u32())
                .collect(),
            update_path: commit.update_path_leaf_node().map(candidate).transpose()?,
        };
        if inspection.additions.len() > MAX_DEVICES
            || inspection.updates.len() > MAX_DEVICES
            || inspection.removals.len() > MAX_DEVICES
        {
            return Err(Error::Limit);
        }
        let stage = StageHandle {
            owner: self.owner,
            generation: self.generation,
            serial: self.serial()?,
        };
        self.stages.insert(
            stage,
            PendingStage {
                group: handle,
                epoch: inspection.from_epoch,
                commit: *commit,
                inspection,
                sender_identity,
                inspected: false,
                authorized: false,
            },
        );
        Ok(stage)
    }
    fn pending(&self, handle: StageHandle) -> Result<&PendingStage> {
        if handle.owner != self.owner || handle.generation != self.generation {
            return Err(Error::StaleHandle);
        }
        let pending = self.stages.get(&handle).ok_or(Error::StaleHandle)?;
        if self.group(pending.group)?.epoch().as_u64() != pending.epoch {
            return Err(Error::StaleHandle);
        }
        Ok(pending)
    }
    pub fn inspect_stage(&mut self, handle: StageHandle) -> Result<StageInspection> {
        let inspection = self.pending(handle)?.inspection.clone();
        self.stages
            .get_mut(&handle)
            .ok_or(Error::StaleHandle)?
            .inspected = true;
        Ok(inspection)
    }
    pub fn authorize_stage(&mut self, handle: StageHandle) -> Result<AuthorizedStageHandle> {
        let pending = self.pending(handle)?;
        if !pending.inspected {
            return Err(Error::NotInspected);
        }
        let group = self.group(pending.group)?;
        for proposal in pending.commit.queued_proposals() {
            if !matches!(
                proposal.proposal(),
                Proposal::Add(_) | Proposal::Update(_) | Proposal::Remove(_)
            ) {
                return Err(Error::UnsupportedPolicyChange);
            }
        }
        let removals = &pending.inspection.removals;
        for member in group.members() {
            if !removals.contains(&member.index.u32()) {
                self.policy.approval(
                    member.credential.serialized_content(),
                    &member.signature_key,
                )?;
            }
        }
        if group
            .members()
            .count()
            .saturating_add(pending.inspection.additions.len())
            > MAX_DEVICES
        {
            return Err(Error::Limit);
        }
        for addition in &pending.inspection.additions {
            self.policy
                .approval(&addition.identity, &addition.public_key)?;
            if group
                .members()
                .any(|m| m.credential.serialized_content() == addition.identity)
            {
                return Err(Error::IdentityChange);
            }
        }
        for proposal in pending.commit.update_proposals() {
            let Sender::Member(sender) = proposal.sender() else {
                return Err(Error::UnsupportedPolicyChange);
            };
            let old = group
                .members()
                .find(|m| m.index == *sender)
                .ok_or(Error::ProtocolRejected)?;
            let leaf = proposal.update_proposal().leaf_node();
            if leaf.credential().serialized_content() != old.credential.serialized_content() {
                return Err(Error::IdentityChange);
            }
            self.policy.approval(
                leaf.credential().serialized_content(),
                leaf.signature_key().as_slice(),
            )?;
        }
        if let Some(path) = &pending.inspection.update_path {
            if path.identity != pending.sender_identity {
                return Err(Error::IdentityChange);
            }
            self.policy.approval(&path.identity, &path.public_key)?;
        }
        self.stages
            .get_mut(&handle)
            .ok_or(Error::StaleHandle)?
            .authorized = true;
        Ok(AuthorizedStageHandle { stage: handle })
    }
    pub fn merge_authorized(&mut self, authorized: AuthorizedStageHandle) -> Result<GroupSummary> {
        let pending = self.pending(authorized.stage)?;
        if !pending.authorized {
            return Err(Error::NotAuthorized);
        }
        let group_handle = pending.group;
        let pending = self
            .stages
            .remove(&authorized.stage)
            .ok_or(Error::StaleHandle)?;
        let group = self
            .groups
            .get_mut(&group_handle)
            .ok_or(Error::StaleHandle)?;
        if group
            .merge_staged_commit(&self.provider, pending.commit)
            .is_err()
        {
            self.quarantined.insert(group_handle);
            return Err(Error::ProviderFailure);
        }
        self.summary(group_handle)
    }
    pub fn reject_stage(&mut self, stage: StageHandle) -> Result<()> {
        self.pending(stage)?;
        self.stages.remove(&stage);
        Ok(())
    }
    pub fn receive_application(&mut self, handle: GroupHandle, wire: &[u8]) -> Result<Vec<u8>> {
        if self.stages.values().any(|stage| stage.group == handle) {
            return Err(Error::Busy);
        }
        let (processed, _) = self.receive(handle, wire, ContentType::Application)?;
        let ProcessedMessageContent::ApplicationMessage(application) = processed.into_content()
        else {
            return Err(Error::UnexpectedMessage);
        };
        Ok(application.into_bytes())
    }
    /// Native account/profile invalidation. Previous handles cannot be reused.
    /// Drops in-memory groups/stages; does not claim durable key erasure.
    pub fn invalidate_generation(&mut self) -> Result<()> {
        self.groups.clear();
        self.stages.clear();
        self.quarantined.clear();
        if self.retired {
            return Err(Error::Retired);
        }
        let Some(next) = self.generation.checked_add(1) else {
            self.retired = true;
            return Err(Error::Limit);
        };
        self.generation = next;
        Ok(())
    }
    /// A native store replacement invalidates all pending approvals before any
    /// future merge. No remote or renderer policy setter is exposed.
    pub fn replace_native_policy(&mut self, policy: NativeTrustPolicy) -> Result<()> {
        if self.retired {
            return Err(Error::Retired);
        }
        if self.scope != policy.scope {
            return Err(Error::ScopeMismatch);
        }
        self.stages.clear();
        self.policy = policy;
        Ok(())
    }
}
