//! Native typed-event admission, using actual relay receipts and SDK proofs.
//! No renderer input constructor, profile invention, foreign-admin boolean,
//! plaintext API fallback or historical ciphertext re-decrypt. The enclosing
//! owner must hold its actual auth/window/epoch publication fence through
//! `commit_with`. Durable SQLCipher history/paging/search is a separate gate;
//! this first bounded owner cache MUST NOT be presented as persistent history.
use super::{Error, Result};
use mnema_crypto_sdk_prototype::{
    ArchivedChatObservation, ChatEventClaim, ChatOperation, NativeProtectedEventScope,
    ReactionAction, VerifiedChatEvent,
};
use mnema_private_native_client_broker::{NativeAuthenticatedScope, NativeOpaqueRecord};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, HashMap};
use uuid::Uuid;
use zeroize::Zeroizing;

const MAX_MESSAGES: usize = 4096;
const MAX_RECEIPTS: usize = 32768;
const MAX_BODY_BYTES: usize = 16 * 1024 * 1024;
const MAX_PAGE: usize = 10; // actual canonical broker Page limit
const MAX_SAFE_NUMBER: i64 = (1i64 << 53) - 1;
const MAX_USER_REACTIONS: usize = 20;
const MAX_MESSAGE_EMOJI: usize = 50;

/// Live receive evidence and saved historical observations remain distinct.
/// Neither supplies mutation permission; the owner checks current auth/SDK.
pub(crate) enum NativeHistoryObservation {
    Live(VerifiedChatEvent),
    Archived(ArchivedChatObservation),
}
impl NativeHistoryObservation {
    pub(crate) fn event_id(&self) -> &str {
        match self {
            Self::Live(v) => v.event_id(),
            Self::Archived(v) => v.event_id(),
        }
    }
    pub(crate) fn account(&self) -> &str {
        match self {
            Self::Live(v) => v.account(),
            Self::Archived(v) => v.account(),
        }
    }
    pub(crate) fn device(&self) -> &str {
        match self {
            Self::Live(v) => v.device(),
            Self::Archived(v) => v.device(),
        }
    }
    pub(crate) fn operation(&self) -> &ChatOperation {
        match self {
            Self::Live(v) => v.operation(),
            Self::Archived(v) => v.operation(),
        }
    }
    fn matches_context(&self, current: &NativeProtectedEventScope) -> bool {
        match self {
            Self::Live(v) => v.scope().matches_exact(current),
            Self::Archived(v) => v.matches_history_context(current),
        }
    }
}

#[derive(Clone)]
struct Reaction {
    present: bool,
    number: i64,
}
/// Exact admitted facts. Profile enrichment must use actual native metadata;
/// no username/avatar/admin flag, invented pin/attachment, or Serialize here.
#[derive(Clone)]
pub(crate) struct NativeHistoryMessage {
    id: Uuid,
    number: i64,
    event: Uuid,
    account: Uuid,
    device: String,
    body: Zeroizing<String>,
    parent: Option<Uuid>,
    quote: Option<Uuid>,
    created_at: String,
    updated_at: String,
    revision: Uuid,
    revision_number: i64,
    revision_account: Uuid,
    revision_device: String,
    edited: bool,
    deleted: bool,
    reactions: BTreeMap<(Uuid, String), Reaction>,
}
impl NativeHistoryMessage {
    pub(crate) fn id(&self) -> Uuid {
        self.id
    }
    pub(crate) fn number(&self) -> i64 {
        self.number
    }
    pub(crate) fn client_event_id(&self) -> Uuid {
        self.event
    }
    pub(crate) fn account(&self) -> Uuid {
        self.account
    }
    pub(crate) fn device(&self) -> &str {
        &self.device
    }
    pub(crate) fn body(&self) -> &str {
        self.body.as_str()
    }
    pub(crate) fn parent_id(&self) -> Option<Uuid> {
        self.parent
    }
    pub(crate) fn reply_to_id(&self) -> Option<Uuid> {
        self.quote
    }
    pub(crate) fn created_at(&self) -> &str {
        &self.created_at
    }
    pub(crate) fn updated_at(&self) -> &str {
        &self.updated_at
    }
    pub(crate) fn revision(&self) -> Uuid {
        self.revision
    }
    pub(crate) fn revision_account(&self) -> Uuid {
        self.revision_account
    }
    pub(crate) fn revision_device(&self) -> &str {
        &self.revision_device
    }
    pub(crate) fn is_edited(&self) -> bool {
        self.edited
    }
    pub(crate) fn is_deleted(&self) -> bool {
        self.deleted
    }
    pub(crate) fn reaction_users(&self) -> BTreeMap<String, Vec<Uuid>> {
        let mut result = BTreeMap::new();
        for ((account, emoji), reaction) in &self.reactions {
            if reaction.present {
                result
                    .entry(emoji.clone())
                    .or_insert_with(Vec::new)
                    .push(*account);
            }
        }
        result
    }
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) enum NativeHistoryChange {
    Created(Uuid),
    Updated(Uuid),
    Deleted(Vec<Uuid>),
    Reaction(Uuid),
    Duplicate,
}
#[derive(Clone)]
struct State {
    messages: HashMap<Uuid, NativeHistoryMessage>,
    receipts: HashMap<Uuid, [u8; 32]>,
    numbers: HashMap<i64, Uuid>,
    events: HashMap<(Uuid, Uuid), Uuid>,
}
/// Created ONLY by canonical native owner from its checked SDK/channel/group.
pub(crate) struct NativeChatHistory {
    owner: Uuid,
    context: Option<NativeProtectedEventScope>,
    channel: Uuid,
    group: Zeroizing<Vec<u8>>,
    generation: u64,
    retired: bool,
    state: State,
}
/// Private staged facts, never renderer-minted/deserializable. Source generation
/// is checked again before publication; staging itself changes no live history.
pub(crate) struct StagedChatHistory {
    owner: Uuid,
    scope: Option<NativeProtectedEventScope>,
    generation: u64,
    state: State,
    changes: Vec<NativeHistoryChange>,
}
impl StagedChatHistory {
    pub(crate) fn message(&self, id: Uuid) -> Option<&NativeHistoryMessage> {
        self.state.messages.get(&id)
    }
    pub(crate) fn changes(&self) -> &[NativeHistoryChange] {
        &self.changes
    }
    pub(crate) fn reply_count(&self, root: Uuid) -> usize {
        self.state
            .messages
            .values()
            .filter(|m| m.parent == Some(root) && !m.deleted)
            .count()
    }
}
// The ONLY operational constructor below obtains these through real native
// receipt getters + actual VerifiedChatEvent. Unit tests may build this PRIVATE
// state input to test policy; they do not prove TLS/MLS provenance.
struct Row {
    id: Uuid,
    number: i64,
    account: Uuid,
    device: String,
    event: Uuid,
    created_at: String,
    operation: ChatOperation,
}
fn canonical(value: &str) -> Result<Uuid> {
    let id = Uuid::parse_str(value).map_err(|_| Error::Invalid)?;
    if id.is_nil() || id.hyphenated().to_string() != value {
        return Err(Error::Invalid);
    }
    Ok(id)
}
fn require_canonical_community(
    origin: &str,
    community: &str,
    expected_origin: &str,
    expected_community: &str,
) -> Result<()> {
    // Exactly the strict conversion used by canonical NativeChatOwner, never
    // ad-hoc trailing-slash trimming or path/query origin broadening.
    let actual = super::Scope::new(origin, community).map_err(|_| Error::Binding)?;
    if actual.origin() != expected_origin || actual.community() != expected_community {
        return Err(Error::Binding);
    }
    Ok(())
}
fn fingerprint(row: &Row) -> [u8; 32] {
    fn part(hash: &mut Sha256, bytes: &[u8]) {
        hash.update((bytes.len() as u64).to_be_bytes());
        hash.update(bytes);
    }
    let mut hash = Sha256::new();
    for bytes in [
        row.id.as_bytes(),
        row.account.as_bytes(),
        row.event.as_bytes(),
    ] {
        part(&mut hash, bytes);
    }
    part(&mut hash, &row.number.to_be_bytes());
    part(&mut hash, row.device.as_bytes());
    part(&mut hash, row.created_at.as_bytes());
    part(&mut hash, &[row.operation.kind() as u8]);
    for field in [
        Some(row.operation.message_id()),
        row.operation.body(),
        row.operation.parent_id(),
        row.operation.reply_to_id(),
        row.operation.expected_revision(),
    ] {
        hash.update([u8::from(field.is_some())]);
        if let Some(field) = field {
            part(&mut hash, field.as_bytes());
        }
    }
    if let ChatOperation::Reaction { emoji, action, .. } = &row.operation {
        part(&mut hash, emoji.as_bytes());
        hash.update([u8::from(*action == ReactionAction::Add)]);
    }
    hash.finalize().into()
}
impl NativeChatHistory {
    pub(crate) fn for_current_native_owner(context: NativeProtectedEventScope) -> Result<Self> {
        let channel = canonical(context.channel())?;
        if context.group().is_empty() || context.group().len() > 128 {
            return Err(Error::Binding);
        }
        let group = Zeroizing::new(context.group().to_vec());
        Ok(Self {
            owner: Uuid::new_v4(),
            context: Some(context),
            channel,
            group,
            generation: 1,
            retired: false,
            state: State {
                messages: HashMap::new(),
                receipts: HashMap::new(),
                numbers: HashMap::new(),
                events: HashMap::new(),
            },
        })
    }
    pub(crate) fn stage_observed<'a>(
        &self,
        current: NativeProtectedEventScope,
        rows: impl IntoIterator<Item = (&'a NativeOpaqueRecord, &'a NativeHistoryObservation)>,
    ) -> Result<StagedChatHistory> {
        if self.retired {
            return Err(Error::Retired);
        }
        let context = self.context.as_ref().ok_or(Error::Binding)?;
        if !context.same_native_context(&current) {
            return Err(Error::Binding);
        }
        let mut admitted = Vec::new();
        for (record, inner) in rows {
            if admitted.len() >= MAX_PAGE
                || !inner.matches_context(&current)
                || record.channel_id() != self.channel
                || record.group_id() != self.group.as_slice()
                || inner.account() != record.account_id().to_string()
                || inner.event_id() != record.client_event_id().to_string()
                || record.number() <= 0
                || record.number() > MAX_SAFE_NUMBER
                || record.created_at().timestamp_millis().unsigned_abs() > 8_640_000_000_000_000
            {
                return Err(Error::Binding);
            }
            admitted.push(Row {
                id: record.id(),
                number: record.number(),
                account: record.account_id(),
                device: inner.device().into(),
                event: record.client_event_id(),
                created_at: record.created_at().to_rfc3339(),
                operation: inner.operation().clone(),
            });
        }
        let mut staged = self.stage_rows(admitted)?;
        staged.scope = Some(current);
        Ok(staged)
    }
    /// Canonical serialized owner calls this BEFORE reserving encryption. The
    /// owner must derive both scopes from the SAME live SDK/broker and recheck
    /// actual own-leaf account binding via from_proven_sdk/scope/current. Scope
    /// getters cannot themselves establish that signer relation or role trust.
    /// This performs no reservation, mutation, timestamp/receipt fabrication or
    /// foreign-admin authorization. Retrying an existing reservation retains its
    /// original opaque handle; it does not call producer admission again.
    pub(crate) fn admit_native_producer(
        &self,
        auth: &NativeAuthenticatedScope,
        current: &NativeProtectedEventScope,
        event: Uuid,
        claim: &ChatEventClaim,
    ) -> Result<()> {
        if self.retired {
            return Err(Error::Retired);
        }
        let context = self.context.as_ref().ok_or(Error::Binding)?;
        require_canonical_community(
            auth.origin(),
            auth.community_id(),
            current.origin(),
            current.community(),
        )?;
        if !context.same_native_context(current)
            || auth.native_profile_identity().to_string() != current.profile()
            || auth.native_window_identity().to_string() != current.window()
            || format!("{}.{}", auth.family_id(), auth.client_instance_id()) != current.session()
            || std::time::Instant::now() >= auth.monotonic_access_deadline()
        {
            return Err(Error::Binding);
        }
        self.state
            .admit_claim(auth.account_id(), event, claim.operation())
    }
    fn stage_rows(&self, rows: Vec<Row>) -> Result<StagedChatHistory> {
        if self.retired {
            return Err(Error::Retired);
        }
        if rows.len() > MAX_PAGE {
            return Err(Error::Limit);
        }
        let mut state = self.state.clone();
        let mut changes = Vec::with_capacity(rows.len());
        for row in rows {
            changes.push(state.apply(row)?);
        }
        if state.messages.len() > MAX_MESSAGES
            || state.receipts.len() > MAX_RECEIPTS
            || state.messages.values().map(|m| m.body.len()).sum::<usize>() > MAX_BODY_BYTES
        {
            return Err(Error::Limit);
        }
        Ok(StagedChatHistory {
            owner: self.owner,
            scope: None,
            generation: self.generation,
            state,
            changes,
        })
    }
    /// Caller MUST hold actual auth/window/MLS lease through this callback.
    /// Enqueue refusal or stale stage changes no live history. Never reuse a
    /// decrypted wire to rebuild a lost/refused stage: retire native owner.
    pub(crate) fn commit_with<T>(
        &mut self,
        staged: StagedChatHistory,
        current: NativeProtectedEventScope,
        publish: impl FnOnce(&StagedChatHistory) -> Result<T>,
    ) -> Result<T> {
        if self.retired {
            return Err(Error::Retired);
        }
        let context = self.context.as_ref().ok_or(Error::Binding)?;
        let captured = staged.scope.as_ref().ok_or(Error::Binding)?;
        if !context.same_native_context(&current) || !captured.matches_exact(&current) {
            return Err(Error::Binding);
        }
        self.commit_state_with(staged, publish)
    }
    // Private state transaction. Operational parent cannot use this to skip
    // actual scope/proof validation; state-only tests never mint SDK facts.
    fn commit_state_with<T>(
        &mut self,
        staged: StagedChatHistory,
        publish: impl FnOnce(&StagedChatHistory) -> Result<T>,
    ) -> Result<T> {
        if self.retired {
            return Err(Error::Retired);
        }
        if staged.owner != self.owner || staged.generation != self.generation {
            return Err(Error::Conflict);
        }
        let generation = self.generation.checked_add(1).ok_or(Error::Limit)?;
        let output = publish(&staged)?;
        self.state = staged.state;
        self.generation = generation;
        Ok(output)
    }
    /// Historical admitted RAM facts, not fresh event proofs. Parent must derive
    /// current from its live SDK and retain real auth/window publication fences.
    pub(crate) fn message_for_native_scope(
        &self,
        current: &NativeProtectedEventScope,
        id: Uuid,
    ) -> Result<Option<&NativeHistoryMessage>> {
        self.require_native_context(current)?;
        Ok(self.state.messages.get(&id))
    }
    /// Historical admitted RAM rows only. Sorting uses real receipt numbers;
    /// neither missing history nor a fresh event proof is manufactured here.
    /// Parent retains actual same-owner auth/SDK checks and the final sink fence.
    pub(crate) fn messages_for_native_scope(
        &self,
        current: &NativeProtectedEventScope,
        parent: Option<Uuid>,
    ) -> Result<Vec<&NativeHistoryMessage>> {
        self.require_native_context(current)?;
        let mut rows: Vec<_> = self
            .state
            .messages
            .values()
            .filter(|message| message.parent == parent)
            .collect();
        rows.sort_unstable_by_key(|message| message.number);
        Ok(rows)
    }
    pub(crate) fn cached_reply_count_for_native_scope(
        &self,
        current: &NativeProtectedEventScope,
        root: Uuid,
    ) -> Result<usize> {
        self.require_native_context(current)?;
        Ok(self
            .state
            .messages
            .values()
            .filter(|m| m.parent == Some(root) && !m.deleted)
            .count())
    }
    fn require_native_context(&self, current: &NativeProtectedEventScope) -> Result<()> {
        if self.retired {
            return Err(Error::Retired);
        }
        if !self
            .context
            .as_ref()
            .ok_or(Error::Binding)?
            .same_native_context(current)
        {
            return Err(Error::Binding);
        }
        Ok(())
    }
    #[cfg(test)]
    fn message(&self, id: Uuid) -> Option<&NativeHistoryMessage> {
        self.state.messages.get(&id)
    }
    pub(crate) fn retire(&mut self) {
        self.owner = Uuid::new_v4(); // burns every pending stage, even same group
        self.retired = true;
        self.context = None;
        self.group.clear();
        self.state.messages.clear();
        self.state.receipts.clear();
        self.state.numbers.clear();
        self.state.events.clear();
    }
}
impl State {
    fn admit_claim(&self, account: Uuid, event: Uuid, operation: &ChatOperation) -> Result<()> {
        if account.is_nil() || event.is_nil() {
            return Err(Error::Invalid);
        }
        if self.events.contains_key(&(account, event)) {
            return Err(Error::Conflict);
        }
        if self.receipts.len() >= MAX_RECEIPTS {
            return Err(Error::Limit);
        }
        let body_bytes: usize = self.messages.values().map(|m| m.body.len()).sum();
        match operation {
            ChatOperation::Create {
                message_id,
                body,
                parent_id,
            }
            | ChatOperation::Reply {
                message_id,
                body,
                parent_id,
                ..
            } => {
                if canonical(message_id)? != event {
                    return Err(Error::Binding);
                }
                let parent = parent_id.as_deref().map(canonical).transpose()?;
                if let Some(parent) = parent {
                    let root = self.messages.get(&parent).ok_or(Error::Binding)?;
                    if root.deleted || root.parent.is_some() {
                        return Err(Error::Binding);
                    }
                }
                if let Some(quote) = operation.reply_to_id() {
                    let quote = canonical(quote)?;
                    let target = self.messages.get(&quote).ok_or(Error::Binding)?;
                    if target.deleted
                        || !(target.parent == parent
                            || (target.parent.is_none() && parent == Some(quote)))
                    {
                        return Err(Error::Binding);
                    }
                }
                if self.messages.len() >= MAX_MESSAGES
                    || body_bytes.saturating_add(body.len()) > MAX_BODY_BYTES
                {
                    return Err(Error::Limit);
                }
            }
            ChatOperation::Edit {
                message_id,
                expected_revision,
                body,
            } => {
                let target = self.claim_author_target(account, message_id, expected_revision)?;
                if body_bytes
                    .saturating_sub(target.body.len())
                    .saturating_add(body.len())
                    > MAX_BODY_BYTES
                {
                    return Err(Error::Limit);
                }
            }
            ChatOperation::Delete {
                message_id,
                expected_revision,
            } => {
                self.claim_author_target(account, message_id, expected_revision)?;
            }
            ChatOperation::Reaction {
                message_id,
                emoji,
                action,
            } => {
                let target = self
                    .messages
                    .get(&canonical(message_id)?)
                    .ok_or(Error::Binding)?;
                if target.deleted {
                    return Err(Error::Binding);
                }
                if *action == ReactionAction::Add {
                    Self::check_reaction_limit(target, account, emoji)?;
                }
            }
        }
        Ok(())
    }
    fn claim_author_target(
        &self,
        account: Uuid,
        message_id: &str,
        expected_revision: &str,
    ) -> Result<&NativeHistoryMessage> {
        let target = self
            .messages
            .get(&canonical(message_id)?)
            .ok_or(Error::Binding)?;
        if target.deleted
            || target.account != account
            || target.revision != canonical(expected_revision)?
        {
            return Err(Error::Binding);
        }
        Ok(target)
    }
    fn check_reaction_limit(
        message: &NativeHistoryMessage,
        account: Uuid,
        emoji: &str,
    ) -> Result<()> {
        if message
            .reactions
            .get(&(account, emoji.into()))
            .is_some_and(|r| r.present)
        {
            return Ok(());
        }
        let mine = message
            .reactions
            .iter()
            .filter(|((a, _), r)| *a == account && r.present)
            .count();
        let emojis: std::collections::HashSet<_> = message
            .reactions
            .iter()
            .filter(|(_, r)| r.present)
            .map(|((_, e), _)| e.as_str())
            .collect();
        if mine >= MAX_USER_REACTIONS
            || (!emojis.contains(emoji) && emojis.len() >= MAX_MESSAGE_EMOJI)
        {
            return Err(Error::Limit);
        }
        Ok(())
    }
    fn apply(&mut self, row: Row) -> Result<NativeHistoryChange> {
        let digest = fingerprint(&row);
        if let Some(original) = self.receipts.get(&row.id) {
            return if original == &digest {
                Ok(NativeHistoryChange::Duplicate)
            } else {
                Err(Error::Conflict)
            };
        }
        if self.receipts.len() >= MAX_RECEIPTS
            || row.id.is_nil()
            || row.account.is_nil()
            || row.event.is_nil()
            || row.number <= 0
            || row.number > MAX_SAFE_NUMBER
        {
            return Err(Error::Limit);
        }
        if self.numbers.contains_key(&row.number)
            || self.events.contains_key(&(row.account, row.event))
        {
            return Err(Error::Conflict);
        }
        let change = match &row.operation {
            ChatOperation::Create {
                message_id,
                body,
                parent_id,
            }
            | ChatOperation::Reply {
                message_id,
                body,
                parent_id,
                ..
            } => {
                if canonical(message_id)? != row.event || self.messages.contains_key(&row.id) {
                    return Err(Error::Binding);
                }
                let parent = parent_id.as_deref().map(canonical).transpose()?;
                if let Some(parent) = parent {
                    let root = self.messages.get(&parent).ok_or(Error::Binding)?;
                    if root.deleted || root.parent.is_some() || root.number >= row.number {
                        return Err(Error::Binding);
                    }
                }
                let quote = row.operation.reply_to_id().map(canonical).transpose()?;
                if let Some(quote) = quote {
                    let original = self.messages.get(&quote).ok_or(Error::Binding)?;
                    // Go permits quoting the thread root from inside its thread.
                    let same_conversation = original.parent == parent
                        || (original.parent.is_none() && parent == Some(quote));
                    if original.deleted || original.number >= row.number || !same_conversation {
                        return Err(Error::Binding);
                    }
                }
                self.messages.insert(
                    row.id,
                    NativeHistoryMessage {
                        id: row.id,
                        number: row.number,
                        event: row.event,
                        account: row.account,
                        device: row.device.clone(),
                        body: Zeroizing::new(body.clone()),
                        parent,
                        quote,
                        created_at: row.created_at.clone(),
                        updated_at: row.created_at.clone(),
                        revision: row.id,
                        revision_number: row.number,
                        revision_account: row.account,
                        revision_device: row.device.clone(),
                        edited: false,
                        deleted: false,
                        reactions: BTreeMap::new(),
                    },
                );
                NativeHistoryChange::Created(row.id)
            }
            ChatOperation::Edit {
                message_id,
                expected_revision,
                body,
            } => {
                let id = canonical(message_id)?;
                let message = self.author_mutation(&row, id, expected_revision)?;
                message.body = Zeroizing::new(body.clone());
                message.edited = true;
                message.revision = row.id;
                message.revision_number = row.number;
                message.revision_account = row.account;
                message.revision_device = row.device.clone();
                message.updated_at = row.created_at.clone();
                NativeHistoryChange::Updated(id)
            }
            ChatOperation::Delete {
                message_id,
                expected_revision,
            } => {
                let id = canonical(message_id)?;
                self.author_mutation(&row, id, expected_revision)?;
                let mut ids: Vec<_> = self
                    .messages
                    .values()
                    .filter(|m| !m.deleted && (m.id == id || m.parent == Some(id)))
                    .map(|m| m.id)
                    .collect();
                ids.sort_unstable();
                for id in &ids {
                    let message = self.messages.get_mut(id).ok_or(Error::Binding)?;
                    message.deleted = true;
                    message.body = Zeroizing::new(String::new());
                    message.reactions.clear();
                    message.revision = row.id;
                    message.revision_number = row.number;
                    message.revision_account = row.account;
                    message.revision_device = row.device.clone();
                    message.updated_at = row.created_at.clone();
                }
                NativeHistoryChange::Deleted(ids)
            }
            ChatOperation::Reaction {
                message_id,
                emoji,
                action,
            } => {
                let id = canonical(message_id)?;
                let message = self.messages.get_mut(&id).ok_or(Error::Binding)?;
                if message.deleted || row.number <= message.number {
                    return Err(Error::Binding);
                }
                let key = (row.account, emoji.clone());
                if message
                    .reactions
                    .get(&key)
                    .is_some_and(|r| row.number <= r.number)
                {
                    return Err(Error::Conflict);
                }
                if *action == ReactionAction::Add
                    && !message.reactions.get(&key).is_some_and(|r| r.present)
                {
                    Self::check_reaction_limit(message, row.account, emoji)?;
                }
                message.reactions.insert(
                    key,
                    Reaction {
                        present: *action == ReactionAction::Add,
                        number: row.number,
                    },
                );
                NativeHistoryChange::Reaction(id)
            }
        };
        self.receipts.insert(row.id, digest);
        self.numbers.insert(row.number, row.id);
        self.events.insert((row.account, row.event), row.id);
        Ok(change)
    }
    fn author_mutation(
        &mut self,
        row: &Row,
        id: Uuid,
        expected_revision: &str,
    ) -> Result<&mut NativeHistoryMessage> {
        let message = self.messages.get_mut(&id).ok_or(Error::Binding)?;
        // Account-level ownership permits another currently approved device of
        // that same account; actual SDK enforces its device/root membership.
        // Foreign admin delete remains DENIED until typed current-role proof.
        if message.deleted
            || message.account != row.account
            || message.revision != canonical(expected_revision)?
            || row.number <= message.revision_number
        {
            return Err(Error::Binding);
        }
        Ok(message)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn id(n: u128) -> Uuid {
        Uuid::from_u128(n)
    }
    #[test]
    fn broker_root_url_normalization_matches_strict_sdk_scope_without_origin_broadening() {
        for origin in [
            "https://fixture.invalid",
            "https://fixture.invalid/",
            "https://FIXTURE.INVALID:443/",
        ] {
            require_canonical_community(
                origin,
                "fixture-community",
                "https://fixture.invalid",
                "fixture-community",
            )
            .unwrap();
        }
        require_canonical_community(
            "https://127.0.0.1:59677/",
            "fixture-community",
            "https://127.0.0.1:59677",
            "fixture-community",
        )
        .unwrap();
        for origin in [
            "https://other.invalid/",
            "https://fixture.invalid:444/",
            "http://fixture.invalid/",
            "https://fixture.invalid/path",
            "https://fixture.invalid/?query=1",
            "https://fixture.invalid/#fragment",
            "https://user@fixture.invalid/",
            "https://fixture.invalid/ ",
            "https://fixture.invalid\\path",
        ] {
            assert!(
                require_canonical_community(
                    origin,
                    "fixture-community",
                    "https://fixture.invalid",
                    "fixture-community"
                )
                .is_err()
            );
        }
        assert!(
            require_canonical_community(
                "https://fixture.invalid/",
                "other-community",
                "https://fixture.invalid",
                "fixture-community"
            )
            .is_err()
        );
        assert!(
            require_canonical_community(
                "https://fixture.invalid/",
                "bad community",
                "https://fixture.invalid",
                "fixture-community"
            )
            .is_err()
        );
    }
    fn history() -> NativeChatHistory {
        // Private cache fixture ONLY: no operational SDK context/proof. Public
        // native admission requires Some(actual opaque context), never this.
        NativeChatHistory {
            owner: Uuid::new_v4(),
            context: None,
            channel: id(1),
            group: Zeroizing::new(b"state-test-group".to_vec()),
            generation: 1,
            retired: false,
            state: State {
                messages: HashMap::new(),
                receipts: HashMap::new(),
                numbers: HashMap::new(),
                events: HashMap::new(),
            },
        }
    }
    fn create(receipt: u128, number: i64, account: u128, event: u128, parent: Option<u128>) -> Row {
        Row {
            id: id(receipt),
            number,
            account: id(account),
            device: "approved-device".into(),
            event: id(event),
            created_at: "2026-10-08T11:22:33+00:00".into(),
            operation: ChatOperation::Create {
                message_id: id(event).to_string(),
                body: "actual verified body".into(),
                parent_id: parent.map(|n| id(n).to_string()),
            },
        }
    }
    fn mutation(receipt: u128, number: i64, account: u128, operation: ChatOperation) -> Row {
        Row {
            id: id(receipt),
            number,
            account: id(account),
            device: "approved-device".into(),
            event: id(receipt + 10000),
            created_at: "2026-10-08T11:23:34+00:00".into(),
            operation,
        }
    }
    fn commit(h: &mut NativeChatHistory, rows: Vec<Row>) -> Result<Vec<NativeHistoryChange>> {
        let staged = h.stage_rows(rows)?;
        h.commit_state_with(staged, |s| Ok(s.changes().to_vec()))
    }
    #[test]
    fn real_field_mapping_no_fake_dates_and_account_event_collision_no_overwrite() {
        let mut h = history();
        commit(
            &mut h,
            vec![create(101, 1, 10, 501, None), create(102, 2, 20, 501, None)],
        )
        .unwrap();
        for (receipt, account) in [(101, 10), (102, 20)] {
            let message = h.message(id(receipt)).unwrap();
            assert_eq!(message.id(), id(receipt));
            assert_eq!(message.client_event_id(), id(501));
            assert_eq!(message.account(), id(account));
            assert_eq!(message.device(), "approved-device");
            assert_eq!(message.body(), "actual verified body");
            assert_eq!(message.created_at(), "2026-10-08T11:22:33+00:00");
            assert_eq!(message.updated_at(), message.created_at());
        }
        assert!(h.stage_rows(vec![create(103, 3, 10, 501, None)]).is_err());
    }
    #[test]
    fn same_account_device_can_edit_but_foreign_author_admin_flag_cannot_be_supplied() {
        let mut h = history();
        commit(&mut h, vec![create(101, 1, 10, 501, None)]).unwrap();
        let edit = |account| {
            mutation(
                102,
                2,
                account,
                ChatOperation::Edit {
                    message_id: id(101).to_string(),
                    expected_revision: id(101).to_string(),
                    body: "edited".into(),
                },
            )
        };
        assert!(h.stage_rows(vec![edit(20)]).is_err());
        let mut row = edit(10);
        row.device = "other-approved-device".into();
        commit(&mut h, vec![row]).unwrap();
        let message = h.message(id(101)).unwrap();
        assert_eq!(message.account(), id(10));
        assert_eq!(message.device(), "approved-device");
        assert_eq!(message.body(), "edited");
        assert!(message.is_edited());
        assert_eq!(message.revision(), id(102));
        assert_eq!(message.revision_account(), id(10));
        assert_eq!(message.revision_device(), "other-approved-device");
        assert_eq!(message.updated_at(), "2026-10-08T11:23:34+00:00");
        assert!(
            h.stage_rows(vec![mutation(
                103,
                3,
                20,
                ChatOperation::Delete {
                    message_id: id(101).to_string(),
                    expected_revision: id(102).to_string()
                }
            )])
            .is_err()
        );
    }
    #[test]
    fn stale_revision_deleted_targets_and_receipt_metadata_replay_are_rejected() {
        let mut h = history();
        let original = create(101, 1, 10, 501, None);
        commit(&mut h, vec![original]).unwrap();
        assert_eq!(
            commit(&mut h, vec![create(101, 1, 10, 501, None)]).unwrap(),
            vec![NativeHistoryChange::Duplicate]
        );
        let mut altered = create(101, 1, 10, 501, None);
        altered.created_at = "2026-10-09T11:22:33+00:00".into();
        assert!(h.stage_rows(vec![altered]).is_err());
        assert!(h.stage_rows(vec![create(102, 1, 20, 502, None)]).is_err());
        commit(
            &mut h,
            vec![mutation(
                102,
                2,
                10,
                ChatOperation::Edit {
                    message_id: id(101).to_string(),
                    expected_revision: id(101).to_string(),
                    body: "changed".into(),
                },
            )],
        )
        .unwrap();
        assert!(
            h.stage_rows(vec![mutation(
                103,
                3,
                10,
                ChatOperation::Delete {
                    message_id: id(101).to_string(),
                    expected_revision: id(101).to_string()
                }
            )])
            .is_err()
        );
        commit(
            &mut h,
            vec![mutation(
                103,
                3,
                10,
                ChatOperation::Delete {
                    message_id: id(101).to_string(),
                    expected_revision: id(102).to_string(),
                },
            )],
        )
        .unwrap();
        assert!(h.message(id(101)).unwrap().is_deleted());
        assert_eq!(h.message(id(101)).unwrap().body(), "");
        assert!(
            h.stage_rows(vec![mutation(
                104,
                4,
                10,
                ChatOperation::Edit {
                    message_id: id(101).to_string(),
                    expected_revision: id(103).to_string(),
                    body: "resurrect".into()
                }
            )])
            .is_err()
        );
    }
    #[test]
    fn thread_quote_relation_and_cascade_delete_preserve_exact_receipt_refs() {
        let mut h = history();
        commit(
            &mut h,
            vec![
                create(101, 1, 10, 501, None),
                create(102, 2, 20, 502, Some(101)),
            ],
        )
        .unwrap();
        assert!(
            h.stage_rows(vec![create(103, 3, 20, 503, Some(102))])
                .is_err()
        );
        let mut quote = create(103, 3, 20, 503, Some(101));
        quote.operation = ChatOperation::Reply {
            message_id: id(503).to_string(),
            body: "quote root".into(),
            parent_id: Some(id(101).to_string()),
            reply_to_id: id(101).to_string(),
        };
        let staged = h.stage_rows(vec![quote]).unwrap();
        assert_eq!(staged.reply_count(id(101)), 2);
        assert_eq!(
            staged.message(id(103)).unwrap().reply_to_id(),
            Some(id(101))
        );
        h.commit_state_with(staged, |_| Ok(())).unwrap();
        let mut cross_thread = create(104, 4, 20, 504, None);
        cross_thread.operation = ChatOperation::Reply {
            message_id: id(504).to_string(),
            body: "cross".into(),
            parent_id: None,
            reply_to_id: id(102).to_string(),
        };
        assert!(h.stage_rows(vec![cross_thread]).is_err());
        let changes = commit(
            &mut h,
            vec![mutation(
                105,
                5,
                10,
                ChatOperation::Delete {
                    message_id: id(101).to_string(),
                    expected_revision: id(101).to_string(),
                },
            )],
        )
        .unwrap();
        assert_eq!(
            changes,
            vec![NativeHistoryChange::Deleted(vec![
                id(101),
                id(102),
                id(103)
            ])]
        );
        for n in [101, 102, 103] {
            assert!(h.message(id(n)).unwrap().is_deleted());
        }
    }
    #[test]
    fn reaction_add_remove_replay_is_account_bound_and_limits_match_existing_web() {
        let mut h = history();
        commit(&mut h, vec![create(101, 1, 10, 501, None)]).unwrap();
        for n in 0..20 {
            commit(
                &mut h,
                vec![mutation(
                    200 + n,
                    2 + n as i64,
                    20,
                    ChatOperation::Reaction {
                        message_id: id(101).to_string(),
                        emoji: format!("r{n}"),
                        action: ReactionAction::Add,
                    },
                )],
            )
            .unwrap();
        }
        assert!(
            h.stage_rows(vec![mutation(
                300,
                50,
                20,
                ChatOperation::Reaction {
                    message_id: id(101).to_string(),
                    emoji: "extra".into(),
                    action: ReactionAction::Add
                }
            )])
            .is_err()
        );
        commit(
            &mut h,
            vec![mutation(
                301,
                51,
                20,
                ChatOperation::Reaction {
                    message_id: id(101).to_string(),
                    emoji: "r0".into(),
                    action: ReactionAction::Remove,
                },
            )],
        )
        .unwrap();
        assert!(
            h.stage_rows(vec![mutation(
                302,
                40,
                20,
                ChatOperation::Reaction {
                    message_id: id(101).to_string(),
                    emoji: "r0".into(),
                    action: ReactionAction::Add
                }
            )])
            .is_err()
        );
        commit(
            &mut h,
            vec![mutation(
                303,
                52,
                30,
                ChatOperation::Reaction {
                    message_id: id(101).to_string(),
                    emoji: "r1".into(),
                    action: ReactionAction::Add,
                },
            )],
        )
        .unwrap();
        assert_eq!(
            h.message(id(101)).unwrap().reaction_users().get("r1"),
            Some(&vec![id(20), id(30)])
        );
        for n in 20..50 {
            commit(
                &mut h,
                vec![mutation(
                    400 + n,
                    100 + n as i64,
                    1000 + n,
                    ChatOperation::Reaction {
                        message_id: id(101).to_string(),
                        emoji: format!("r{n}"),
                        action: ReactionAction::Add,
                    },
                )],
            )
            .unwrap();
        }
        // r0 is removed, so restore it from another approved account: 50 types.
        commit(
            &mut h,
            vec![mutation(
                500,
                200,
                40,
                ChatOperation::Reaction {
                    message_id: id(101).to_string(),
                    emoji: "r0".into(),
                    action: ReactionAction::Add,
                },
            )],
        )
        .unwrap();
        assert!(
            h.stage_rows(vec![mutation(
                501,
                201,
                50,
                ChatOperation::Reaction {
                    message_id: id(101).to_string(),
                    emoji: "fiftyfirst".into(),
                    action: ReactionAction::Add
                }
            )])
            .is_err()
        );
    }
    #[test]
    fn producer_preflight_uses_known_receipts_author_revision_thread_and_no_state_mutation() {
        let mut h = history();
        commit(
            &mut h,
            vec![create(101, 1, 10, 501, None), create(102, 2, 20, 502, None)],
        )
        .unwrap();
        let edit = ChatEventClaim::claim(ChatOperation::Edit {
            message_id: id(101).to_string(),
            expected_revision: id(101).to_string(),
            body: "edit".into(),
        })
        .unwrap();
        assert!(
            h.state
                .admit_claim(id(10), id(701), edit.operation())
                .is_ok()
        );
        assert!(
            h.state
                .admit_claim(id(20), id(701), edit.operation())
                .is_err()
        );
        let foreign_delete = ChatEventClaim::claim(ChatOperation::Delete {
            message_id: id(101).to_string(),
            expected_revision: id(101).to_string(),
        })
        .unwrap();
        assert!(
            h.state
                .admit_claim(id(20), id(702), foreign_delete.operation())
                .is_err()
        );
        let stale = ChatEventClaim::claim(ChatOperation::Edit {
            message_id: id(101).to_string(),
            expected_revision: id(999).to_string(),
            body: "stale".into(),
        })
        .unwrap();
        assert!(
            h.state
                .admit_claim(id(10), id(703), stale.operation())
                .is_err()
        );
        let reply = ChatEventClaim::claim(ChatOperation::Reply {
            message_id: id(704).to_string(),
            body: "thread".into(),
            parent_id: Some(id(101).to_string()),
            reply_to_id: id(101).to_string(),
        })
        .unwrap();
        assert!(
            h.state
                .admit_claim(id(20), id(704), reply.operation())
                .is_ok()
        );
        assert!(
            h.state
                .admit_claim(id(20), id(705), reply.operation())
                .is_err()
        );
        let cross_thread = ChatEventClaim::claim(ChatOperation::Reply {
            message_id: id(706).to_string(),
            body: "cross".into(),
            parent_id: Some(id(101).to_string()),
            reply_to_id: id(102).to_string(),
        })
        .unwrap();
        assert!(
            h.state
                .admit_claim(id(20), id(706), cross_thread.operation())
                .is_err()
        );
        assert_eq!(h.message(id(101)).unwrap().body(), "actual verified body");
        assert_eq!(h.state.receipts.len(), 2);
        assert_eq!(h.state.messages.len(), 2);
        assert!(!h.state.events.contains_key(&(id(10), id(701))));
        assert!(
            h.state
                .admit_claim(id(10), id(501), edit.operation())
                .is_err()
        );
        commit(
            &mut h,
            vec![mutation(201, 3, 10, foreign_delete.operation().clone())],
        )
        .unwrap();
        assert!(
            h.state
                .admit_claim(id(10), id(707), edit.operation())
                .is_err()
        );
        assert!(
            h.state
                .admit_claim(id(20), id(704), reply.operation())
                .is_err()
        );
    }
    #[test]
    fn producer_preflight_reaction_limits_share_incoming_policy_and_allow_explicit_remove() {
        let mut h = history();
        commit(&mut h, vec![create(101, 1, 10, 501, None)]).unwrap();
        for n in 0..20 {
            commit(
                &mut h,
                vec![mutation(
                    201 + n,
                    2 + n as i64,
                    20,
                    ChatOperation::Reaction {
                        message_id: id(101).to_string(),
                        emoji: format!("r{n}"),
                        action: ReactionAction::Add,
                    },
                )],
            )
            .unwrap();
        }
        let claim = |emoji: &str, action| {
            ChatEventClaim::claim(ChatOperation::Reaction {
                message_id: id(101).to_string(),
                emoji: emoji.into(),
                action,
            })
            .unwrap()
        };
        assert!(
            h.state
                .admit_claim(
                    id(20),
                    id(701),
                    claim("overflow", ReactionAction::Add).operation()
                )
                .is_err()
        );
        assert!(
            h.state
                .admit_claim(
                    id(20),
                    id(701),
                    claim("r0", ReactionAction::Add).operation()
                )
                .is_ok()
        );
        assert!(
            h.state
                .admit_claim(
                    id(20),
                    id(701),
                    claim("r0", ReactionAction::Remove).operation()
                )
                .is_ok()
        );
        assert!(
            h.state
                .admit_claim(
                    id(30),
                    id(701),
                    claim("overflow", ReactionAction::Add).operation()
                )
                .is_ok()
        );
        assert_eq!(h.message(id(101)).unwrap().reaction_users().len(), 20);
        for n in 20..50 {
            commit(
                &mut h,
                vec![mutation(
                    201 + n,
                    2 + n as i64,
                    100 + n,
                    ChatOperation::Reaction {
                        message_id: id(101).to_string(),
                        emoji: format!("r{n}"),
                        action: ReactionAction::Add,
                    },
                )],
            )
            .unwrap();
        }
        assert!(
            h.state
                .admit_claim(
                    id(30),
                    id(701),
                    claim("overflow", ReactionAction::Add).operation()
                )
                .is_err()
        );
        assert!(
            h.state
                .admit_claim(
                    id(30),
                    id(701),
                    claim("r0", ReactionAction::Add).operation()
                )
                .is_ok()
        );
        assert!(
            h.state
                .admit_claim(
                    id(30),
                    id(701),
                    claim("overflow", ReactionAction::Remove).operation()
                )
                .is_ok()
        );
    }
    #[test]
    fn atomic_page_final_enqueue_failure_stale_stages_and_owner_retirement_do_not_commit() {
        let mut h = history();
        assert!(
            h.stage_rows(vec![
                create(101, 1, 10, 501, None),
                create(102, 2, 20, 502, Some(999))
            ])
            .is_err()
        );
        assert!(h.message(id(101)).is_none());
        let refused = h.stage_rows(vec![create(101, 1, 10, 501, None)]).unwrap();
        assert!(
            h.commit_state_with(refused, |_| Err::<(), _>(Error::Auth))
                .is_err()
        );
        assert!(h.message(id(101)).is_none());
        let old = h.stage_rows(vec![create(101, 1, 10, 501, None)]).unwrap();
        commit(&mut h, vec![create(102, 2, 20, 502, None)]).unwrap();
        assert!(
            h.commit_state_with(old, |_| -> Result<()> {
                panic!("stale stage must not publish")
            })
            .is_err()
        );
        let pending = h.stage_rows(vec![create(101, 1, 10, 501, None)]).unwrap();
        h.retire();
        assert!(
            h.commit_state_with(pending, |_| -> Result<()> {
                panic!("retired stage must not publish")
            })
            .is_err()
        );
        assert!(h.message(id(102)).is_none());
        assert!(
            h.stage_rows(
                (0..11)
                    .map(|n| create(100 + n, 1 + n as i64, 10, 500 + n, None))
                    .collect()
            )
            .is_err()
        );
    }
}
