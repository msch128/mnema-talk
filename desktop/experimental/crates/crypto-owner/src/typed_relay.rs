//! Same actual native owner, typed MLS events and real blind-relay receipts.
//! This bounded RAM history is not persistent/recoverable application history.
//! No raw ciphertext/verified-proof constructor or renderer authority is exposed.
use super::*;
use chat_history::{
    NativeHistoryChange, NativeHistoryMessage, NativeHistoryObservation, StagedChatHistory,
};
use mnema_crypto_sdk_prototype::{
    ChatEventClaim, ChatOperation, NativeProtectedEventScope, ProtectedReceived,
};
use mnema_private_native_client_broker::{NativeOpaqueEvent, NativeOpaqueRelayOperation};
use serde::Serialize;
use std::collections::BTreeMap;

/// Exact admitted facts only. Profile/role enrichment needs actual native API
/// provenance; timestamps are real relay receipts, not signed sender clocks.
#[derive(Serialize)]
pub struct NativeTypedChatMessage {
    pub id: Uuid,
    pub number: i64,
    pub client_event_id: Uuid,
    pub account_id: Uuid,
    pub device_id: String,
    pub body: String,
    pub parent_id: Option<Uuid>,
    pub reply_to_id: Option<Uuid>,
    pub created_at: String,
    pub updated_at: String,
    pub revision_id: Uuid,
    pub revision_account_id: Uuid,
    pub revision_device_id: String,
    pub edited: bool,
    pub deleted: bool,
    pub reactions: BTreeMap<String, Vec<Uuid>>,
    pub cached_reply_count: usize,
}
#[derive(Serialize)]
#[serde(tag = "kind", content = "messages", rename_all = "snake_case")]
pub enum NativeTypedChatChange {
    Created(Vec<NativeTypedChatMessage>),
    Updated(Vec<NativeTypedChatMessage>),
    Deleted(Vec<NativeTypedChatMessage>),
    Reaction(Vec<NativeTypedChatMessage>),
    Duplicate,
}
#[derive(Serialize)]
pub struct NativeChatEventPublication {
    pub channel_id: Uuid,
    /// Exact authenticated event-to-relay receipt association, including an
    /// idempotent ACK with no new history changes. Never inferred from body text.
    pub receipts: Vec<NativeChatEventReceipt>,
    pub changes: Vec<NativeTypedChatChange>,
}
/// Routing/view association of an already admitted actual event and receipt.
/// `message_id` is the original global message receipt, not the event UUID.
#[derive(Serialize)]
pub struct NativeChatEventReceipt {
    pub id: Uuid,
    pub number: i64,
    pub client_event_id: Uuid,
    pub account_id: Uuid,
    pub message_id: Uuid,
}
fn project_receipt(
    record: &mnema_private_native_client_broker::NativeOpaqueRecord,
    event: &NativeHistoryObservation,
) -> Result<NativeChatEventReceipt> {
    let message_id = match event.operation() {
        ChatOperation::Create { .. } | ChatOperation::Reply { .. } => record.id(),
        operation => Uuid::parse_str(operation.message_id()).map_err(|_| Error::Binding)?,
    };
    Ok(NativeChatEventReceipt {
        id: record.id(),
        number: record.number(),
        client_event_id: record.client_event_id(),
        account_id: record.account_id(),
        message_id,
    })
}
/// Bounded previously admitted RAM history. This never rewinds the MLS receive
/// cursor, loads unknown old ciphertext or labels restored state as fresh proof.
#[derive(Serialize)]
pub struct NativeChatHistoryPublication {
    pub channel_id: Uuid,
    pub parent_id: Option<Uuid>,
    pub has_older: bool,
    pub next_before: Option<i64>,
    pub messages: Vec<NativeTypedChatMessage>,
}
/// One owner job and exact ciphertext. Never Serde, Clone or renderer input.
pub struct NativePreparedChatEvent {
    auth: Arc<NativeAuthenticatedScope>,
    event: Uuid,
    scope: NativeProtectedEventScope,
    ciphertext: Zeroizing<Vec<u8>>,
    owner: Uuid,
}
fn same_operation(a: &ChatOperation, b: &ChatOperation) -> bool {
    if a.kind() != b.kind()
        || a.message_id() != b.message_id()
        || a.body() != b.body()
        || a.parent_id() != b.parent_id()
        || a.reply_to_id() != b.reply_to_id()
        || a.expected_revision() != b.expected_revision()
    {
        return false;
    }
    match (a, b) {
        (
            ChatOperation::Reaction {
                emoji: a,
                action: aa,
                ..
            },
            ChatOperation::Reaction {
                emoji: b,
                action: ba,
                ..
            },
        ) => a == b && aa == ba,
        _ => true,
    }
}
fn project_message(
    row: &NativeHistoryMessage,
    cached_reply_count: usize,
) -> NativeTypedChatMessage {
    NativeTypedChatMessage {
        id: row.id(),
        number: row.number(),
        client_event_id: row.client_event_id(),
        account_id: row.account(),
        device_id: row.device().into(),
        body: row.body().into(),
        parent_id: row.parent_id(),
        reply_to_id: row.reply_to_id(),
        created_at: row.created_at().into(),
        updated_at: row.updated_at().into(),
        revision_id: row.revision(),
        revision_account_id: row.revision_account(),
        revision_device_id: row.revision_device().into(),
        edited: row.is_edited(),
        deleted: row.is_deleted(),
        reactions: row.reaction_users(),
        cached_reply_count,
    }
}
fn project_changes(
    channel: Uuid,
    staged: &StagedChatHistory,
    receipts: Vec<NativeChatEventReceipt>,
) -> Result<NativeChatEventPublication> {
    let mut changes = Vec::with_capacity(staged.changes().len());
    for change in staged.changes() {
        let message = |id: Uuid| {
            staged
                .message(id)
                .map(|m| project_message(m, staged.reply_count(m.id())))
                .ok_or(Error::Binding)
        };
        changes.push(match change {
            NativeHistoryChange::Created(id) => NativeTypedChatChange::Created(vec![message(*id)?]),
            NativeHistoryChange::Updated(id) => NativeTypedChatChange::Updated(vec![message(*id)?]),
            NativeHistoryChange::Reaction(id) => {
                NativeTypedChatChange::Reaction(vec![message(*id)?])
            }
            NativeHistoryChange::Deleted(ids) => NativeTypedChatChange::Deleted(
                ids.iter()
                    .map(|id| message(*id))
                    .collect::<Result<Vec<_>>>()?,
            ),
            NativeHistoryChange::Duplicate => NativeTypedChatChange::Duplicate,
        });
    }
    let publication = NativeChatEventPublication {
        channel_id: channel,
        receipts,
        changes,
    };
    // Bound the actual display projection before entering the broker's bounded
    // final enqueue closure. A huge cascade cannot hold that lock indefinitely.
    let encoded = Zeroizing::new(serde_json::to_vec(&publication).map_err(|_| Error::Invalid)?);
    if encoded.len() > 512 * 1024 {
        return Err(Error::Limit);
    }
    Ok(publication)
}
impl NativeChatOwner {
    /// Read only previously admitted historical RAM facts. This does not
    /// manufacture a fresh event proof, recover a database snapshot or fetch an
    /// unknown message. Actual current native auth/window remains mandatory.
    pub fn publish_cached_chat_event_message<T>(
        &mut self,
        id: Uuid,
        publish: impl FnOnce(&NativeTypedChatMessage) -> Result<T>,
    ) -> Result<Option<T>> {
        let admitted = Arc::new(self.scope()?);
        self.publish_cached_chat_event_message_retained(admitted, id, publish)
    }
    pub fn publish_cached_chat_event_message_retained<T>(
        &mut self,
        auth: Arc<NativeAuthenticatedScope>,
        id: Uuid,
        publish: impl FnOnce(&NativeTypedChatMessage) -> Result<T>,
    ) -> Result<Option<T>> {
        self.check_admitted_scope(&auth)?;
        if id.is_nil() {
            return Err(Error::Invalid);
        }
        self.enter_chat_protocol(true)?;
        self.current(now()?)?;
        let scope = self
            .sdk
            .native_protected_event_scope(now()?)
            .map_err(|_| Error::Crypto)?;
        let Some(row) = self.typed_history.message_for_native_scope(&scope, id)? else {
            return Ok(None);
        };
        let replies = self
            .typed_history
            .cached_reply_count_for_native_scope(&scope, id)?;
        let projection = project_message(row, replies);
        let encoded = Zeroizing::new(serde_json::to_vec(&projection).map_err(|_| Error::Invalid)?);
        if encoded.len() > 512 * 1024 {
            return Err(Error::Limit);
        }
        self.client
            .with_authenticated_publication(&self.lease, &auth, || publish(&projection))
            .map_err(|_| Error::Auth)?
            .map(Some)
    }
    pub fn publish_cached_chat_event_history_retained<T>(
        &mut self,
        auth: Arc<NativeAuthenticatedScope>,
        parent: Option<Uuid>,
        publish: impl FnOnce(&NativeChatHistoryPublication) -> Result<T>,
    ) -> Result<T> {
        self.publish_cached_chat_event_history_page_retained(auth, parent, None, 25, publish)
    }
    /// Paging applies only to already authenticated local history. `before` is
    /// an actual original receipt number, never an MLS receive/decrypt cursor.
    pub fn publish_cached_chat_event_history_page_retained<T>(
        &mut self,
        auth: Arc<NativeAuthenticatedScope>,
        parent: Option<Uuid>,
        before: Option<i64>,
        limit: usize,
        publish: impl FnOnce(&NativeChatHistoryPublication) -> Result<T>,
    ) -> Result<T> {
        self.check_admitted_scope(&auth)?;
        if parent.is_some_and(|id| id.is_nil())
            || before.is_some_and(|n| n <= 0)
            || !(1..=25).contains(&limit)
        {
            return Err(Error::Invalid);
        }
        self.enter_chat_protocol(true)?;
        self.current(now()?)?;
        let scope = self
            .sdk
            .native_protected_event_scope(now()?)
            .map_err(|_| Error::Crypto)?;
        let mut rows = self
            .typed_history
            .messages_for_native_scope(&scope, parent)?;
        rows.retain(|row| before.is_none_or(|n| row.number() < n));
        let has_older = rows.len() > limit;
        let start = rows.len().saturating_sub(limit);
        let messages = rows[start..]
            .iter()
            .map(|row| {
                let replies = self
                    .typed_history
                    .cached_reply_count_for_native_scope(&scope, row.id())?;
                Ok(project_message(row, replies))
            })
            .collect::<Result<Vec<_>>>()?;
        let mut projection = NativeChatHistoryPublication {
            channel_id: self.owner.channel,
            parent_id: parent,
            has_older,
            next_before: messages.first().map(|m| m.number),
            messages,
        };
        loop {
            let encoded =
                Zeroizing::new(serde_json::to_vec(&projection).map_err(|_| Error::Invalid)?);
            if encoded.len() <= 512 * 1024 {
                break;
            }
            if projection.messages.len() <= 1 {
                return Err(Error::Limit);
            }
            projection.messages.remove(0);
            projection.has_older = true;
            projection.next_before = projection.messages.first().map(|m| m.number);
        }
        self.check_admitted_scope(&auth)?;
        let fresh = self
            .sdk
            .native_protected_event_scope(now()?)
            .map_err(|_| Error::Crypto)?;
        if !scope.matches_exact(&fresh) {
            return Err(Error::Binding);
        }
        self.client
            .with_authenticated_publication(&self.lease, &auth, || publish(&projection))
            .map_err(|_| Error::Auth)?
    }
    pub(super) fn enter_chat_protocol(&mut self, typed: bool) -> Result<()> {
        if self.retired {
            return Err(Error::Retired);
        }
        if self.chat_protocol.is_some_and(|current| current != typed) {
            return Err(Error::Conflict);
        }
        self.chat_protocol = Some(typed);
        Ok(())
    }
    /// Producer claims are admitted by actual current own native history/account
    /// BEFORE any provider mutation. Repeating an exact reserved operation uses
    /// the same opaque reservation and ciphertext, not another encryption.
    pub fn prepare_chat_event(
        &mut self,
        event: Uuid,
        claim: &ChatEventClaim,
    ) -> Result<NativePreparedChatEvent> {
        let admitted = Arc::new(self.scope()?);
        self.prepare_chat_event_retained(admitted, event, claim)
    }
    pub fn prepare_chat_event_retained(
        &mut self,
        auth: Arc<NativeAuthenticatedScope>,
        event: Uuid,
        claim: &ChatEventClaim,
    ) -> Result<NativePreparedChatEvent> {
        self.check_admitted_scope(&auth)?;
        if event.is_nil() {
            return Err(Error::Invalid);
        }
        self.enter_chat_protocol(true)?;
        let when = now()?;
        self.current(when)?;
        let scope = self
            .sdk
            .native_protected_event_scope(when)
            .map_err(|_| Error::Crypto)?;
        if let Some(reserved) = self.typed_events.get(&event) {
            let existing = self
                .sdk
                .pending_native_chat_event_projection(reserved, when)
                .map_err(|_| Error::Crypto)?;
            if !same_operation(existing.operation(), claim.operation()) {
                return Err(Error::Conflict);
            }
        } else {
            if self.events.contains_key(&event) {
                return Err(Error::Conflict);
            }
            if self.events.len() + self.typed_events.len() >= 256 {
                return Err(Error::Limit);
            }
            self.typed_history
                .admit_native_producer(&auth, &scope, event, claim)?;
            let reserved = match self
                .sdk
                .reserve_native_chat_event(&event.to_string(), claim, when)
            {
                Ok(value) => value,
                Err(_) => {
                    self.retire();
                    return Err(Error::Crypto);
                }
            };
            self.typed_events.insert(event, reserved);
        }
        // Provider/SQL writes ran outside broker locks. Recheck the captured
        // native auth and exact crypto state before releasing any HTTP job.
        self.check_admitted_scope(&auth)?;
        self.client
            .check_authenticated_scope(&self.lease, &auth)
            .map_err(|_| Error::Auth)?;
        let fresh = self
            .sdk
            .native_protected_event_scope(now()?)
            .map_err(|_| Error::Crypto)?;
        if !scope.matches_exact(&fresh) {
            self.retire();
            return Err(Error::Binding);
        }
        let reserved = self.typed_events.get(&event).ok_or(Error::Binding)?;
        self.sdk
            .pending_native_chat_event_projection(reserved, now()?)
            .map_err(|_| Error::Crypto)?;
        Ok(NativePreparedChatEvent {
            auth,
            event,
            scope: fresh,
            ciphertext: Zeroizing::new(reserved.wire_for_native_relay().to_vec()),
            owner: self.serial,
        })
    }
    pub fn validate_prepared_chat_event(&mut self, job: &NativePreparedChatEvent) -> Result<()> {
        self.check_admitted_scope(&job.auth)?;
        if job.owner != self.serial || self.chat_protocol != Some(true) {
            return Err(Error::Binding);
        }
        self.client
            .check_authenticated_scope(&self.lease, &job.auth)
            .map_err(|_| Error::Auth)?;
        self.current(now()?)?;
        let scope = self
            .sdk
            .native_protected_event_scope(now()?)
            .map_err(|_| Error::Crypto)?;
        if !job.scope.matches_exact(&scope) {
            return Err(Error::Binding);
        }
        let reserved = self.typed_events.get(&job.event).ok_or(Error::Binding)?;
        self.sdk
            .pending_native_chat_event_projection(reserved, now()?)
            .map_err(|_| Error::Crypto)?;
        if reserved.wire_for_native_relay() != job.ciphertext.as_slice() {
            return Err(Error::Binding);
        }
        Ok(())
    }
    pub async fn publish_chat_event<T>(
        &mut self,
        job: NativePreparedChatEvent,
        publish: impl FnOnce(&NativeChatEventPublication) -> Result<T>,
    ) -> Result<T> {
        self.validate_prepared_chat_event(&job)?;
        let input = NativeOpaqueEvent::from_native_outbox(
            self.owner.channel,
            job.event,
            &self.owner.group,
            &job.ciphertext,
        )
        .map_err(|_| Error::Invalid)?;
        let client = self.client.clone();
        let lease = self.lease.clone();
        // An uncertain network result preserves this actual reservation. Caller
        // may prepare exactly the same operation to obtain a fresh auth job.
        let response = client
            .request_opaque_relay_retained(
                &lease,
                job.auth.clone(),
                NativeOpaqueRelayOperation::Publish(input),
            )
            .await
            .map_err(|_| Error::Auth)?;
        let result = (|| {
            self.check_admitted_scope(&job.auth)?;
            self.current(now()?)?;
            let receipt = response.opaque_relay_receipt().ok_or(Error::Binding)?;
            if !receipt.is_publish_ack() || receipt.records().len() != 1 {
                return Err(Error::Binding);
            }
            let record = &receipt.records()[0];
            let reserved = self.typed_events.get(&job.event).ok_or(Error::Binding)?;
            if record.account_id() != self.owner.account
                || record.client_event_id() != job.event
                || record.channel_id() != self.owner.channel
                || record.group_id() != self.owner.group
                || record.ciphertext() != reserved.wire_for_native_relay()
            {
                return Err(Error::Binding);
            }
            let proof = self
                .sdk
                .pending_native_chat_event_projection(reserved, now()?)
                .map_err(|_| Error::Crypto)?;
            if !proof.scope().matches_exact(&job.scope) {
                return Err(Error::Binding);
            }
            let proof = NativeHistoryObservation::Live(proof);
            let scope = self
                .sdk
                .native_protected_event_scope(now()?)
                .map_err(|_| Error::Crypto)?;
            let staged = self
                .typed_history
                .stage_observed(scope, [(record, &proof)])?;
            let projection = project_changes(
                self.owner.channel,
                &staged,
                vec![project_receipt(record, &proof)?],
            )?;
            let final_scope = self
                .sdk
                .native_protected_event_scope(now()?)
                .map_err(|_| Error::Crypto)?;
            self.typed_history.commit_with(staged, final_scope, |_| {
                client
                    .with_opaque_relay_publication(&lease, response, |_| publish(&projection))
                    .map_err(|_| Error::Auth)?
            })
        })();
        if result.is_err() {
            self.retire();
        }
        result
    }
    /// Exact saved observations are historical, never a live reservation grant.
    /// Every unknown wire reaches ONE actual SDK receive. A same-account other
    /// device never inherits an own reservation's projection bypass.
    pub async fn receive_chat_event_page<T>(
        &mut self,
        publish: impl FnOnce(&NativeChatEventPublication) -> Result<T>,
    ) -> Result<T> {
        let admitted = Arc::new(self.scope()?);
        self.receive_chat_event_page_retained(admitted, publish)
            .await
    }
    pub async fn receive_chat_event_page_retained<T>(
        &mut self,
        auth: Arc<NativeAuthenticatedScope>,
        publish: impl FnOnce(&NativeChatEventPublication) -> Result<T>,
    ) -> Result<T> {
        self.check_admitted_scope(&auth)?;
        self.enter_chat_protocol(true)?;
        self.current(now()?)?;
        let client = self.client.clone();
        let lease = self.lease.clone();
        let after = self.cursor;
        let response = client
            .request_opaque_relay_retained(
                &lease,
                auth.clone(),
                NativeOpaqueRelayOperation::Page {
                    channel: self.owner.channel,
                    after,
                },
            )
            .await
            .map_err(|_| Error::Auth)?;
        let result = (|| {
            self.check_admitted_scope(&auth)?;
            self.current(now()?)?;
            let receipt = response.opaque_relay_receipt().ok_or(Error::Binding)?;
            if receipt.is_publish_ack() {
                return Err(Error::Binding);
            }
            let mut verified: Vec<NativeHistoryObservation> =
                Vec::with_capacity(receipt.records().len());
            let mut next = after;
            for record in receipt.records() {
                if record.channel_id() != self.owner.channel
                    || record.group_id() != self.owner.group
                    || record.number() <= next
                {
                    return Err(Error::Binding);
                }
                next = record.number();
                let own = if record.account_id() == self.owner.account {
                    self.typed_events
                        .get(&record.client_event_id())
                        .filter(|reserved| reserved.wire_for_native_relay() == record.ciphertext())
                } else {
                    None
                };
                let proof = if let Some(archived) = self
                    .sdk
                    .archived_chat_observation(
                        &record.account_id().to_string(),
                        &record.client_event_id().to_string(),
                        record.ciphertext(),
                    )
                    .map_err(|_| Error::Crypto)?
                {
                    NativeHistoryObservation::Archived(archived)
                } else if let Some(own) = own {
                    NativeHistoryObservation::Live(
                        self.sdk
                            .pending_native_chat_event_projection(own, now()?)
                            .map_err(|_| Error::Crypto)?,
                    )
                } else {
                    match self
                        .sdk
                        .receive_protected(
                            &record.client_event_id().to_string(),
                            record.ciphertext(),
                            now()?,
                        )
                        .map_err(|_| Error::Crypto)?
                    {
                        ProtectedReceived::ChatEvent(event) => {
                            NativeHistoryObservation::Live(*event)
                        }
                        _ => return Err(Error::Binding), // no alternate domain/decrypt fallback.
                    }
                };
                if proof.account() != record.account_id().to_string() {
                    return Err(Error::Binding);
                }
                verified.push(proof);
            }
            if receipt.next_after() != next {
                return Err(Error::Binding);
            }
            let scope = self
                .sdk
                .native_protected_event_scope(now()?)
                .map_err(|_| Error::Crypto)?;
            let staged = self
                .typed_history
                .stage_observed(scope, receipt.records().iter().zip(verified.iter()))?;
            let acknowledgements = receipt
                .records()
                .iter()
                .zip(verified.iter())
                .map(|(record, proof)| project_receipt(record, proof))
                .collect::<Result<Vec<_>>>()?;
            let projection = project_changes(self.owner.channel, &staged, acknowledgements)?;
            let final_scope = self
                .sdk
                .native_protected_event_scope(now()?)
                .map_err(|_| Error::Crypto)?;
            let value = self.typed_history.commit_with(staged, final_scope, |_| {
                client
                    .with_opaque_relay_publication(&lease, response, |_| publish(&projection))
                    .map_err(|_| Error::Auth)?
            })?;
            self.cursor = next;
            Ok(value)
        })();
        // A provider receive already consumed ratchet state. No retry after
        // admission/final enqueue error and no cursor advancement on refusal.
        if result.is_err() {
            self.retire();
        }
        result
    }
}
