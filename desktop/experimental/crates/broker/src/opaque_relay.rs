//! Native transport for already-protected bytes. This module does not establish
//! MLS trust, membership or an authenticated inner author. Only a trusted native
//! Core adapter may call it from its current durable outbox, or decrypt a result.
//! There is deliberately no Deserialize, Serialize or renderer command surface.
use super::*;
use base64::{Engine, engine::general_purpose::STANDARD};
use chrono::{DateTime, FixedOffset};
use serde::{Deserialize, Serialize};

/// Native-only transport object. It cannot be minted by renderer JSON.
/// ```compile_fail
/// use mnema_private_native_client_broker::NativeOpaqueEvent;
/// let _=serde_json::from_str::<NativeOpaqueEvent>("{}");
/// ```
pub struct NativeOpaqueEvent {
    channel: Uuid,
    event: Uuid,
    group: Zeroizing<Vec<u8>>,
    ciphertext: Zeroizing<Vec<u8>>,
}
impl fmt::Debug for NativeOpaqueEvent {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeOpaqueEvent(REDACTED)")
    }
}
impl NativeOpaqueEvent {
    /// Bounds and canonical routing only. This constructor is NOT a Core grant.
    /// The caller must provide exact current durable SDK outbox bytes; a lost ACK
    /// must never cause a new encryption or replacement client event identity.
    pub fn from_native_outbox(
        channel: Uuid,
        event: Uuid,
        group: &[u8],
        ciphertext: &[u8],
    ) -> Result<Self, Error> {
        if channel.is_nil()
            || event.is_nil()
            || group.is_empty()
            || group.len() > 128
            || ciphertext.is_empty()
            || ciphertext.len() > 65536
        {
            return Err(Error::InvalidInput);
        }
        Ok(Self {
            channel,
            event,
            group: Zeroizing::new(group.to_vec()),
            ciphertext: Zeroizing::new(ciphertext.to_vec()),
        })
    }
}
pub enum NativeOpaqueRelayOperation {
    Publish(NativeOpaqueEvent),
    Page { channel: Uuid, after: i64 },
}
impl fmt::Debug for NativeOpaqueRelayOperation {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeOpaqueRelayOperation(REDACTED)")
    }
}
pub struct NativeOpaqueRecord {
    id: Uuid,
    number: i64,
    channel: Uuid,
    account: Uuid,
    event: Uuid,
    group: Zeroizing<Vec<u8>>,
    ciphertext: Zeroizing<Vec<u8>>,
    created_at: DateTime<FixedOffset>,
}
impl fmt::Debug for NativeOpaqueRecord {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeOpaqueRecord(REDACTED)")
    }
}
impl NativeOpaqueRecord {
    pub fn id(&self) -> Uuid {
        self.id
    }
    pub fn number(&self) -> i64 {
        self.number
    }
    pub fn channel_id(&self) -> Uuid {
        self.channel
    }
    /// Server bearer metadata, NOT an authenticated MLS sender. The Core adapter
    /// must compare it with the verified inner author and signed current roster.
    pub fn account_id(&self) -> Uuid {
        self.account
    }
    pub fn client_event_id(&self) -> Uuid {
        self.event
    }
    pub fn group_id(&self) -> &[u8] {
        &self.group
    }
    pub fn ciphertext(&self) -> &[u8] {
        &self.ciphertext
    }
    pub fn created_at(&self) -> DateTime<FixedOffset> {
        self.created_at
    }
}
/// Native-only receipt, never a generic renderer response.
/// ```compile_fail
/// use mnema_private_native_client_broker::NativeOpaqueReceipt;
/// fn cannot_export(receipt:&NativeOpaqueReceipt){let _=serde_json::to_vec(receipt);}
/// ```
pub struct NativeOpaqueReceipt {
    records: Vec<NativeOpaqueRecord>,
    next_after: i64,
    published: bool,
}
impl fmt::Debug for NativeOpaqueReceipt {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeOpaqueReceipt(REDACTED)")
    }
}
impl NativeOpaqueReceipt {
    pub fn records(&self) -> &[NativeOpaqueRecord] {
        &self.records
    }
    pub fn next_after(&self) -> i64 {
        self.next_after
    }
    pub fn is_publish_ack(&self) -> bool {
        self.published
    }
}
pub struct NativeOpaqueDelivery {
    operation: Operation,
    scope: Arc<NativeAuthenticatedScope>,
    receipt: NativeOpaqueReceipt,
}
impl fmt::Debug for NativeOpaqueDelivery {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeOpaqueDelivery(REDACTED)")
    }
}
impl NativeOpaqueDelivery {
    /// Native inspection only. No result is admitted to the renderer by this
    /// accessor. Verify Core/inner authentication before final fenced enqueue.
    pub fn receipt(&self) -> &NativeOpaqueReceipt {
        &self.receipt
    }
    pub(super) fn native_binding(&self) -> Option<(Uuid, u64)> {
        self.scope.client_binding
    }
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct WireRecord {
    id: String,
    number: i64,
    channel_id: String,
    user_id: String,
    client_event_id: String,
    group_id: String,
    ciphertext: String,
    created_at: DateTime<FixedOffset>,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct WirePage {
    events: Vec<WireRecord>,
    next_after: i64,
}
fn uuid(value: &str) -> Result<Uuid, Error> {
    let id = Uuid::parse_str(value).map_err(|_| Error::Protocol)?;
    if id.is_nil() || id.hyphenated().to_string() != value {
        return Err(Error::Protocol);
    }
    Ok(id)
}
fn decode(value: &str, max: usize) -> Result<Zeroizing<Vec<u8>>, Error> {
    if value.is_empty() || value.len() > max.div_ceil(3) * 4 {
        return Err(Error::Protocol);
    }
    let bytes = Zeroizing::new(STANDARD.decode(value).map_err(|_| Error::Protocol)?);
    if bytes.is_empty() || bytes.len() > max || STANDARD.encode(bytes.as_slice()) != value {
        return Err(Error::Protocol);
    }
    Ok(bytes)
}
impl WireRecord {
    fn validate(self, channel: Uuid) -> Result<NativeOpaqueRecord, Error> {
        let record = NativeOpaqueRecord {
            id: uuid(&self.id)?,
            number: self.number,
            channel: uuid(&self.channel_id)?,
            account: uuid(&self.user_id)?,
            event: uuid(&self.client_event_id)?,
            group: decode(&self.group_id, 128)?,
            ciphertext: decode(&self.ciphertext, 65536)?,
            created_at: self.created_at,
        };
        if record.channel != channel || record.number <= 0 {
            return Err(Error::Protocol);
        }
        Ok(record)
    }
}
#[derive(Serialize)]
struct PublishWire<'a> {
    client_event_id: String,
    group_id: &'a str,
    ciphertext: &'a str,
}
impl Broker {
    pub async fn opaque_relay(
        &self,
        owner: WindowOwner,
        scope: NativeAuthenticatedScope,
        input: NativeOpaqueRelayOperation,
    ) -> Result<NativeOpaqueDelivery, Error> {
        self.opaque_relay_retained(owner, Arc::new(scope), input)
            .await
    }
    pub(super) async fn opaque_relay_retained(
        &self,
        owner: WindowOwner,
        scope: Arc<NativeAuthenticatedScope>,
        input: NativeOpaqueRelayOperation,
    ) -> Result<NativeOpaqueDelivery, Error> {
        let (endpoint, body, expected, after) = match &input {
            NativeOpaqueRelayOperation::Publish(event) => {
                let group = Zeroizing::new(STANDARD.encode(event.group.as_slice()));
                let ciphertext = Zeroizing::new(STANDARD.encode(event.ciphertext.as_slice()));
                let bytes = Zeroizing::new(
                    serde_json::to_vec(&PublishWire {
                        client_event_id: event.event.hyphenated().to_string(),
                        group_id: &group,
                        ciphertext: &ciphertext,
                    })
                    .map_err(|_| Error::InvalidInput)?,
                );
                (
                    Endpoint::OpaquePost(event.channel),
                    Some(bytes),
                    event.channel,
                    0,
                )
            }
            NativeOpaqueRelayOperation::Page { channel, after } => {
                if channel.is_nil() || *after < 0 {
                    return Err(Error::InvalidInput);
                }
                (
                    Endpoint::OpaquePage(*channel, *after),
                    None,
                    *channel,
                    *after,
                )
            }
        };
        let op = {
            let mut state = self.inner.state.lock().map_err(|_| Error::Internal)?;
            state.check_authenticated(self.inner.owner, owner, &scope)?;
            self.reserve(&mut state, owner, RequestPhase::Running)?
        };
        let grant = op.grant.as_ref().ok_or(Error::ReauthRequired)?;
        let response = match self
            .send(&op, endpoint, Some(&grant.access_token), body)
            .await
        {
            Err(Error::Unauthorized) => {
                let mut state = self.inner.state.lock().map_err(|_| Error::Internal)?;
                state.current(self.inner.owner, &op)?;
                // Current scope must still match; an older401 never revokes a
                // replacement identity/rotation, even on the same profile.
                state.check_authenticated(self.inner.owner, owner, &scope)?;
                state.cancel_requests(None);
                if let Some(active) = state.active.as_mut() {
                    active.cancel.cancel();
                    active.status = Status::ReauthRequired;
                    active.session = None;
                    if let Some(flight) = active.flight.take() {
                        flight.finish(Err(Error::Unauthorized));
                    }
                }
                return Err(Error::Unauthorized);
            }
            result => result?,
        };
        let receipt = match &input {
            NativeOpaqueRelayOperation::Publish(event) => {
                let record = response.decode::<WireRecord>()?.validate(expected)?;
                if record.account != scope.account_id()
                    || record.event != event.event
                    || record.group.as_slice() != event.group.as_slice()
                    || record.ciphertext.as_slice() != event.ciphertext.as_slice()
                {
                    return Err(Error::Protocol);
                }
                NativeOpaqueReceipt {
                    next_after: record.number,
                    records: vec![record],
                    published: true,
                }
            }
            NativeOpaqueRelayOperation::Page { .. } => {
                let page = response.decode::<WirePage>()?;
                if page.events.len() > 10 || page.next_after < after {
                    return Err(Error::Protocol);
                }
                let mut records = Vec::with_capacity(page.events.len());
                let mut cursor = after;
                let mut record_ids = std::collections::HashSet::new();
                let mut event_ids = std::collections::HashSet::new();
                for wire in page.events {
                    let record = wire.validate(expected)?;
                    if record.number <= cursor
                        || !record_ids.insert(record.id)
                        || !event_ids.insert((record.account, record.event))
                    {
                        return Err(Error::Protocol);
                    }
                    cursor = record.number;
                    records.push(record);
                }
                if page.next_after != cursor {
                    return Err(Error::Protocol);
                }
                NativeOpaqueReceipt {
                    records,
                    next_after: cursor,
                    published: false,
                }
            }
        };
        {
            let mut state = self.inner.state.lock().map_err(|_| Error::Internal)?;
            state.current(self.inner.owner, &op)?;
            state.check_authenticated(self.inner.owner, owner, &scope)?;
            state.requests.get_mut(&op.id).ok_or(Error::Stale)?.phase = RequestPhase::Delivered;
        }
        Ok(NativeOpaqueDelivery {
            operation: op,
            scope,
            receipt,
        })
    }
    /// Trusted native bounded nonblocking enqueue only, after actual Core proof.
    /// This protects transport ownership; it cannot turn ciphertext into a grant.
    /// Caller must not re-enter client/broker/registry or take another owner lock.
    pub fn with_opaque_relay_publication<T>(
        &self,
        owner: WindowOwner,
        delivery: NativeOpaqueDelivery,
        publish: impl FnOnce(&NativeOpaqueReceipt) -> T,
    ) -> Result<T, Error> {
        let state = self.inner.state.lock().map_err(|_| Error::Internal)?;
        if owner != delivery.operation.window {
            return Err(Error::Denied);
        }
        state.current(self.inner.owner, &delivery.operation)?;
        state.check_authenticated(self.inner.owner, owner, &delivery.scope)?;
        let output = publish(&delivery.receipt);
        state.check_authenticated(self.inner.owner, owner, &delivery.scope)?;
        Ok(output)
    }
}
