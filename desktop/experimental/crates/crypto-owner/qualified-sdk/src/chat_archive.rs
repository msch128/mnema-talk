//! Durable authenticated chat payloads in the existing SQLCipher database.
//! These rows preserve cryptographic observations, not mutation permissions or
//! live sender state. Native delivery/history admission remains a separate step.
use super::{Error, NativeProtectedEventScope, Result, VerifiedChatEvent, protected::Receipt};
use coset::cbor::value::Value;
use rusqlite::{Connection, OptionalExtension, Transaction, params};
use sha2::{Digest, Sha256};

const MAX_RECORDS: i64 = 32768;
const MAX_PLAINTEXT_BYTES: i64 = 16 * 1024 * 1024;

/// A previously authenticated observation from this native encrypted archive.
/// No public constructor, Clone, Serde or conversion to a live event scope.
/// Epoch/generation describe the observation, never current membership.
pub struct ArchivedChatObservation {
    core_owner: u64,
    origin: String,
    community: String,
    channel: String,
    group: Vec<u8>,
    event: String,
    account: String,
    device: String,
    epoch: u64,
    generation: u64,
    claim: super::ChatEventClaim,
}
impl ArchivedChatObservation {
    pub fn event_id(&self) -> &str {
        &self.event
    }
    pub fn account(&self) -> &str {
        &self.account
    }
    pub fn device(&self) -> &str {
        &self.device
    }
    pub fn epoch(&self) -> u64 {
        self.epoch
    }
    pub fn root_generation(&self) -> u64 {
        self.generation
    }
    pub fn operation(&self) -> &super::ChatOperation {
        self.claim.operation()
    }
    /// Historical routing identity only. Equality confers no current authority.
    /// The native owner must retain current broker/SDK publication fences.
    pub fn matches_history_context(&self, current: &NativeProtectedEventScope) -> bool {
        current.belongs_to_core(self.core_owner)
            && self.origin == current.origin()
            && self.community == current.community()
            && self.channel == current.channel()
            && self.group == current.group()
    }
}

impl super::Core {
    /// Inspect only an exact event/ciphertext association already committed by
    /// this core's authenticated receive/send. Never decrypts or resumes MLS.
    /// Reopened inspection-only cores remain ineligible for all mutations.
    pub fn archived_chat_observation(
        &self,
        account: &str,
        event: &str,
        wire: &[u8],
    ) -> Result<Option<ArchivedChatObservation>> {
        super::id(account)?;
        super::protected::uuid(event)?;
        super::bound_wire(wire)?;
        super::check_pin(&self.connection, &self.pin)?;
        super::check_revision(&self.connection, self.revision)?;
        type Row = (String, Vec<u8>, Vec<u8>, String, String, Vec<u8>, Vec<u8>);
        let row: Option<Row> = self.connection.query_row(
            "SELECT channel,epoch,generation,account,device,wire_hash,plaintext FROM core_chat_archive WHERE account=? AND event=?",
            params![account,event], |r| Ok((r.get(0)?,r.get(1)?,r.get(2)?,r.get(3)?,r.get(4)?,r.get(5)?,r.get(6)?))
        ).optional().map_err(|_| Error::Database)?;
        let Some((channel, epoch, generation, account, device, hash, plaintext)) = row else {
            return Ok(None);
        };
        if hash.as_slice() != Sha256::digest(wire).as_slice() {
            return Err(Error::Unauthorized);
        }
        super::protected::uuid(&channel)?;
        super::id(&account)?;
        super::id(&device)?;
        super::protected::scan(&plaintext)?;
        let value: Value =
            coset::cbor::de::from_reader(plaintext.as_slice()).map_err(|_| Error::Invalid)?;
        if super::protected::encode(&value)? != plaintext {
            return Err(Error::Invalid);
        }
        let Value::Array(mut fields) = value else {
            return Err(Error::Invalid);
        };
        if fields.len() != 10 {
            return Err(Error::Invalid);
        }
        let payload = fields.pop().ok_or(Error::Invalid)?;
        if fields
            != vec![
                Value::Text(super::CHAT_EVENT_DOMAIN.into()),
                Value::Integer(1.into()),
                Value::Text(self.pin.scope.origin().into()),
                Value::Text(self.pin.scope.community().into()),
                Value::Text(channel.clone()),
                Value::Bytes(self.pin.group.clone()),
                Value::Text(account.clone()),
                Value::Text(device.clone()),
                Value::Text(event.into()),
            ]
        {
            return Err(Error::Unauthorized);
        }
        let claim = super::ChatEventClaim::from_authenticated_payload(event, &payload)?;
        Ok(Some(ArchivedChatObservation {
            core_owner: self.owner,
            origin: self.pin.scope.origin().into(),
            community: self.pin.scope.community().into(),
            channel,
            group: self.pin.group.clone(),
            event: event.into(),
            account,
            device,
            epoch: u64::from_be_bytes(epoch.try_into().map_err(|_| Error::Invalid)?),
            generation: u64::from_be_bytes(generation.try_into().map_err(|_| Error::Invalid)?),
            claim,
        }))
    }
}
impl super::Sdk {
    /// Native history observation only; enclosing owner must check its current
    /// actual authentication and SDK before admission and final publication.
    pub fn archived_chat_observation(
        &self,
        account: &str,
        event: &str,
        wire: &[u8],
    ) -> Result<Option<ArchivedChatObservation>> {
        self.core.archived_chat_observation(account, event, wire)
    }
}

pub(super) fn schema(connection: &Connection) -> Result<()> {
    connection
        .execute_batch(
            "CREATE TABLE core_chat_archive(
          event TEXT NOT NULL CHECK(length(event)=36),
          channel TEXT NOT NULL CHECK(length(channel)=36),
          epoch BLOB NOT NULL CHECK(length(epoch)=8),
          generation BLOB NOT NULL CHECK(length(generation)=8),
          account TEXT NOT NULL,
          device TEXT NOT NULL,
          sender INTEGER NOT NULL CHECK(sender>=0),
          wire_hash BLOB NOT NULL CHECK(length(wire_hash)=32),
          plaintext BLOB NOT NULL CHECK(length(plaintext) BETWEEN 1 AND 32768),
          PRIMARY KEY(account,event)
        );",
        )
        .map_err(|_| Error::Database)
}

/// Called by the sole receive dispatcher before its receiver transaction commits.
/// The scope comes from the current SDK, and author facts come from real MLS.
pub(super) fn record_if_typed_chat(
    tx: &Transaction<'_>,
    scope: NativeProtectedEventScope,
    event: &str,
    wire: &[u8],
    receipt: &Receipt,
) -> Result<()> {
    super::protected::scan(&receipt.plaintext)?;
    let value: Value =
        coset::cbor::de::from_reader(receipt.plaintext.as_slice()).map_err(|_| Error::Invalid)?;
    let Value::Array(mut fields) = value else {
        return Err(Error::Invalid);
    };
    if !matches!(fields.first(), Some(Value::Text(domain)) if domain == super::CHAT_EVENT_DOMAIN) {
        return Ok(());
    }
    // Require the exact canonical envelope, including actual authenticated
    // account/device. A recognizable domain string alone is insufficient.
    if fields.len() != 10 {
        return Err(Error::Invalid);
    }
    let payload = fields.pop().ok_or(Error::Invalid)?;
    let expected = vec![
        Value::Text(super::CHAT_EVENT_DOMAIN.into()),
        Value::Integer(1.into()),
        Value::Text(scope.origin().into()),
        Value::Text(scope.community().into()),
        Value::Text(scope.channel().into()),
        Value::Bytes(scope.group().into()),
        Value::Text(receipt.account.clone()),
        Value::Text(receipt.device.clone()),
        Value::Text(event.into()),
    ];
    if fields != expected {
        return Err(Error::Unauthorized);
    }
    let canonical = Value::Array([expected, vec![payload.clone()]].concat());
    if super::protected::encode(&canonical)? != receipt.plaintext {
        return Err(Error::Invalid);
    }
    let verified = VerifiedChatEvent::from_authenticated_payload(
        scope,
        event,
        &receipt.account,
        &receipt.device,
        &payload,
    )?;
    let scope = verified.scope();
    let wire_hash = Sha256::digest(wire);
    let (records, bytes): (i64, i64) = tx
        .query_row(
            "SELECT COUNT(*),COALESCE(SUM(length(plaintext)),0) FROM core_chat_archive",
            [],
            |row| Ok((row.get(0)?, row.get(1)?)),
        )
        .map_err(|_| Error::Database)?;
    if records >= MAX_RECORDS || bytes > MAX_PLAINTEXT_BYTES - receipt.plaintext.len() as i64 {
        return Err(Error::Limit);
    }
    super::require_one(
        tx.execute(
            "INSERT INTO core_chat_archive VALUES(?,?,?,?,?,?,?,?,?)",
            params![
                event,
                scope.channel(),
                scope.epoch().to_be_bytes().as_slice(),
                scope.root_generation().to_be_bytes().as_slice(),
                verified.account(),
                verified.device(),
                receipt.sender,
                wire_hash.as_slice(),
                receipt.plaintext
            ],
        )
        .map_err(|_| Error::Database)?,
    )
}
