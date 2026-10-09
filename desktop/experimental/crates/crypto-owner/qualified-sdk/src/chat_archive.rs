//! Durable authenticated chat payloads in the existing SQLCipher database.
//! These rows preserve cryptographic observations, not mutation permissions or
//! live sender state. Native delivery/history admission remains a separate step.
use super::{Error, NativeProtectedEventScope, Result, VerifiedChatEvent, protected::Receipt};
use coset::cbor::value::Value;
use rusqlite::{Connection, Transaction, params};
use sha2::{Digest, Sha256};

const MAX_RECORDS: i64 = 32768;
const MAX_PLAINTEXT_BYTES: i64 = 16 * 1024 * 1024;

pub(super) fn schema(connection: &Connection) -> Result<()> {
    connection
        .execute_batch(
            "CREATE TABLE core_chat_archive(
          event TEXT PRIMARY KEY CHECK(length(event)=36),
          channel TEXT NOT NULL CHECK(length(channel)=36),
          epoch BLOB NOT NULL CHECK(length(epoch)=8),
          generation BLOB NOT NULL CHECK(length(generation)=8),
          account TEXT NOT NULL,
          device TEXT NOT NULL,
          sender INTEGER NOT NULL CHECK(sender>=0),
          wire_hash BLOB NOT NULL CHECK(length(wire_hash)=32),
          plaintext BLOB NOT NULL CHECK(length(plaintext) BETWEEN 1 AND 32768)
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
