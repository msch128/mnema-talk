use super::{pairing::*, *};
use coset::cbor::value::Value;
use ed25519_dalek::SigningKey;
use mnema_crypto_adapter_candidate::Scope;
use mnema_crypto_trust_candidate::{NativeGroupFloor, TrustVerifier};
use rusqlite::{Connection, OptionalExtension, TransactionBehavior, params};
use sha2::{Digest, Sha256};
use std::path::Path;
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Mode {
    FreshNative,
    Inspection,
    Quarantined,
}
pub struct Issuer<'a, S: NativeSecrets> {
    connection: Connection,
    secrets: &'a S,
    pin: NativeRootPin,
    mode: Mode,
}
impl<'a, S: NativeSecrets> Issuer<'a, S> {
    #[cfg(test)]
    pub(crate) fn fixture_connection(&self) -> &Connection {
        &self.connection
    }
    pub fn provision_fresh(
        path: &Path,
        secrets: &'a S,
        scope: Scope,
        group: &[u8],
    ) -> Result<Self> {
        // Provisioning cannot reuse an existing root or reset its generation ledger.
        let authority = secrets.create_seed("issuer-root")?;
        secrets.create_seed("database-key")?;
        let pin = NativeRootPin::from_out_of_band(scope, group, authority)?;
        let connection = open(path, secrets, true)?;
        connection.execute_batch("CREATE TABLE issuer_state(id INTEGER PRIMARY KEY CHECK(id=1),generation INTEGER NOT NULL CHECK(generation>=0),observed_time INTEGER NOT NULL CHECK(observed_time>=0),origin TEXT NOT NULL,community TEXT NOT NULL,group_id BLOB NOT NULL,authority BLOB NOT NULL);CREATE TABLE invitations(nonce BLOB PRIMARY KEY CHECK(length(nonce)=32),wire BLOB NOT NULL CHECK(length(wire) BETWEEN 1 AND 8192),used INTEGER NOT NULL CHECK(used IN(0,1)),revoked INTEGER NOT NULL CHECK(revoked IN(0,1)),account TEXT NOT NULL,device TEXT NOT NULL,identity BLOB NOT NULL,key BLOB NOT NULL CHECK(length(key)=32));CREATE TABLE members(account TEXT NOT NULL,device TEXT NOT NULL,identity BLOB NOT NULL UNIQUE,key BLOB NOT NULL UNIQUE CHECK(length(key)=32),PRIMARY KEY(account,device));CREATE TABLE roster_outbox(nonce BLOB PRIMARY KEY CHECK(length(nonce)=32),generation INTEGER NOT NULL,proof_hash BLOB NOT NULL CHECK(length(proof_hash)=32),wire BLOB NOT NULL CHECK(length(wire) BETWEEN 1 AND 131072));").map_err(|_|Error::Database)?;
        require_one(
            connection
                .execute(
                    "INSERT INTO issuer_state VALUES(1,0,0,?,?,?,?)",
                    params![
                        pin.scope.origin(),
                        pin.scope.community(),
                        pin.group,
                        pin.authority.as_slice()
                    ],
                )
                .map_err(|_| Error::Database)?,
        )?;
        Ok(Self {
            connection,
            secrets,
            pin,
            mode: Mode::FreshNative,
        })
    }
    pub fn restore_inspection_only(
        path: &Path,
        secrets: &'a S,
        pin: NativeRootPin,
    ) -> Result<Self> {
        let connection = open(path, secrets, false)?;
        let matched:bool=connection.query_row("SELECT COUNT(*)=1 FROM issuer_state WHERE id=1 AND origin=? AND community=? AND group_id=? AND authority=?",params![pin.scope.origin(),pin.scope.community(),pin.group,pin.authority.as_slice()],|r|r.get(0)).map_err(|_|Error::Database)?;
        if !matched {
            return Err(Error::Trust);
        }
        Ok(Self {
            connection,
            secrets,
            pin,
            mode: Mode::Inspection,
        })
    }
    pub fn pin_for_native_out_of_band_transfer(&self) -> NativeRootPin {
        self.pin.clone()
    }
    fn live(&self) -> Result<()> {
        match self.mode {
            Mode::FreshNative => Ok(()),
            Mode::Inspection => Err(Error::Restored),
            Mode::Quarantined => Err(Error::Database),
        }
    }
    fn quarantine<T>(&mut self, result: Result<T>) -> Result<T> {
        if matches!(&result, Err(Error::Database | Error::Trust | Error::Limit)) {
            self.mode = Mode::Quarantined;
        }
        result
    }
    fn root_signer(&self) -> Result<SigningKey> {
        let seed = self.secrets.read_seed("issuer-root")?;
        let signer = SigningKey::from_bytes(&seed);
        if signer.verifying_key().to_bytes() != self.pin.authority {
            return Err(Error::Trust);
        }
        Ok(signer)
    }
    pub fn invite_native_reviewed_device(
        &mut self,
        intent: NativeAdmissionIntent,
        now: u64,
    ) -> Result<Vec<u8>> {
        self.live()?;
        let result = self.invite_inner(intent, now);
        self.quarantine(result)
    }
    fn invite_inner(&mut self, intent: NativeAdmissionIntent, now: u64) -> Result<Vec<u8>> {
        let signer = self.root_signer()?;
        let mut nonce = [0; 32];
        getrandom::fill(&mut nonce).map_err(|_| Error::Random)?;
        let wire = make_invitation(&self.pin, &intent, nonce, now, &signer)?;
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_time(&tx, now)?;
        let count: i64 = tx
            .query_row("SELECT COUNT(*) FROM invitations", [], |r| r.get(0))
            .map_err(|_| Error::Database)?;
        if count >= 256 {
            return Err(Error::Limit);
        }
        require_one(
            tx.execute(
                "INSERT INTO invitations VALUES(?,?,0,0,?,?,?,?)",
                params![
                    nonce.as_slice(),
                    wire,
                    intent.account,
                    intent.device,
                    intent.identity,
                    intent.key.as_slice()
                ],
            )
            .map_err(|_| Error::Database)?,
        )?;
        observe(&tx, now)?;
        tx.commit().map_err(|_| Error::Database)?;
        Ok(wire)
    }
    pub fn admit_proven_device(&mut self, response: &ProofResponse, now: u64) -> Result<Vec<u8>> {
        self.live()?;
        // All untrusted parsing/signature/shape checks precede database mutation.
        let invitation = verify_response(&self.pin, response, now)?;
        let result = self.admit_inner(response, invitation, now);
        self.quarantine(result)
    }
    fn admit_inner(
        &mut self,
        response: &ProofResponse,
        inv: Invitation,
        now: u64,
    ) -> Result<Vec<u8>> {
        let signer = self.root_signer()?;
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_time(&tx, now)?;
        let stored: Option<(Vec<u8>, bool, bool)> = tx
            .query_row(
                "SELECT wire,used,revoked FROM invitations WHERE nonce=?",
                [inv.nonce.as_slice()],
                |r| Ok((r.get(0)?, r.get(1)?, r.get(2)?)),
            )
            .optional()
            .map_err(|_| Error::Database)?;
        let Some((stored, used, revoked)) = stored else {
            return Err(Error::Replay);
        };
        if revoked {
            return Err(Error::Replay);
        }
        if stored != response.invitation {
            return Err(Error::Trust);
        }
        let hash: [u8; 32] = Sha256::digest(&response.proof).into();
        if used {
            let previous: Option<(Vec<u8>, Vec<u8>, i64)> = tx
                .query_row(
                    "SELECT proof_hash,wire,generation FROM roster_outbox WHERE nonce=?",
                    [inv.nonce.as_slice()],
                    |r| Ok((r.get(0)?, r.get(1)?, r.get(2)?)),
                )
                .optional()
                .map_err(|_| Error::Database)?;
            let current: i64 = tx
                .query_row("SELECT generation FROM issuer_state WHERE id=1", [], |r| {
                    r.get(0)
                })
                .map_err(|_| Error::Database)?;
            let wire = match previous {
                Some((old, wire, generation)) if old == hash && generation == current => wire,
                _ => return Err(Error::Replay),
            };
            observe(&tx, now)?;
            tx.commit().map_err(|_| Error::Database)?;
            return Ok(wire);
        }
        let conflicts: i64 = tx.query_row(
            "SELECT COUNT(*) FROM members WHERE key=? OR identity=? OR (account=? AND device=?)",
            params![inv.key.as_slice(), inv.identity, inv.account, inv.device],
            |r| r.get(0),
        ).map_err(|_| Error::Database)?;
        if conflicts != 0 {
            return Err(Error::Replay);
        }
        let count: i64 = tx
            .query_row("SELECT COUNT(*) FROM members", [], |r| r.get(0))
            .map_err(|_| Error::Database)?;
        if count >= 128 {
            return Err(Error::Limit);
        }
        require_one(
            tx.execute(
                "INSERT INTO members VALUES(?,?,?,?)",
                params![inv.account, inv.device, inv.identity, inv.key.as_slice()],
            )
            .map_err(|_| Error::Database)?,
        )?;
        let (generation, wire) = issue_roster(&tx, &self.pin, &signer, now)?;
        #[cfg(test)]
        fixture_exit("after-roster");
        if tx
            .execute(
                "UPDATE invitations SET used=1 WHERE nonce=? AND used=0",
                [inv.nonce.as_slice()],
            )
            .map_err(|_| Error::Database)?
            != 1
        {
            return Err(Error::Database);
        }
        require_one(
            tx.execute(
                "INSERT INTO roster_outbox VALUES(?,?,?,?)",
                params![inv.nonce.as_slice(), generation, hash.as_slice(), wire],
            )
            .map_err(|_| Error::Database)?,
        )?;
        observe(&tx, now)?;
        #[cfg(test)]
        fixture_exit("after-outbox");
        tx.commit().map_err(|_| Error::Database)?;
        #[cfg(test)]
        fixture_exit("after-commit");
        Ok(wire)
    }
    pub fn inspect_issued_roster(&self, nonce: &[u8; 32]) -> Result<Option<Vec<u8>>> {
        self.connection
            .query_row(
                "SELECT wire FROM roster_outbox WHERE nonce=?",
                [nonce.as_slice()],
                |r| r.get(0),
            )
            .optional()
            .map_err(|_| Error::Database)
    }
    pub fn revoke_exact_native_device(
        &mut self,
        intent: NativeAdmissionIntent,
        now: u64,
    ) -> Result<Vec<u8>> {
        self.live()?;
        let result = self.revoke_inner(intent, now);
        self.quarantine(result)
    }
    fn revoke_inner(&mut self, intent: NativeAdmissionIntent, now: u64) -> Result<Vec<u8>> {
        let signer = self.root_signer()?;
        let mut nonce = [0; 32];
        getrandom::fill(&mut nonce).map_err(|_| Error::Random)?;
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_time(&tx, now)?;
        let present: bool = tx.query_row("SELECT EXISTS(SELECT 1 FROM members WHERE account=? AND device=? AND identity=? AND key=?)", params![intent.account,intent.device,intent.identity,intent.key.as_slice()],|r|r.get(0)).map_err(|_|Error::Database)?;
        if !present {
            return Err(Error::Replay);
        }
        if tx
            .execute(
                "DELETE FROM members WHERE account=? AND device=? AND identity=? AND key=?",
                params![
                    intent.account,
                    intent.device,
                    intent.identity,
                    intent.key.as_slice()
                ],
            )
            .map_err(|_| Error::Database)?
            != 1
        {
            return Err(Error::Database);
        }
        let outstanding:i64=tx.query_row("SELECT COUNT(*) FROM invitations WHERE used=0 AND (key=? OR (account=? AND device=?))",params![intent.key.as_slice(),intent.account,intent.device],|r|r.get(0)).map_err(|_|Error::Database)?;
        let invalidated=tx.execute(
            "UPDATE invitations SET revoked=1 WHERE used=0 AND (key=? OR (account=? AND device=?))",
            params![intent.key.as_slice(), intent.account, intent.device],
        )
        .map_err(|_| Error::Database)?;
        if i64::try_from(invalidated).map_err(|_| Error::Limit)? != outstanding {
            return Err(Error::Database);
        }
        let (generation, wire) = issue_roster(&tx, &self.pin, &signer, now)?;
        let event_hash: [u8; 32] =
            Sha256::digest(b"native exact-device revocation audit event").into();
        require_one(
            tx.execute(
                "INSERT INTO roster_outbox VALUES(?,?,?,?)",
                params![nonce.as_slice(), generation, event_hash.as_slice(), wire],
            )
            .map_err(|_| Error::Database)?,
        )?;
        observe(&tx, now)?;
        tx.commit().map_err(|_| Error::Database)?;
        Ok(wire)
    }
}
fn require_one(changed: usize) -> Result<()> {
    if changed == 1 {
        Ok(())
    } else {
        Err(Error::Database)
    }
}
fn clock(now: u64) -> Result<i64> {
    i64::try_from(now).map_err(|_| Error::Invalid)
}
fn check_time(conn: &Connection, now: u64) -> Result<()> {
    let last: i64 = conn
        .query_row(
            "SELECT observed_time FROM issuer_state WHERE id=1",
            [],
            |r| r.get(0),
        )
        .map_err(|_| Error::Database)?;
    if clock(now)? < last {
        Err(Error::Replay)
    } else {
        Ok(())
    }
}
fn observe(conn: &Connection, now: u64) -> Result<()> {
    let changed = conn
        .execute(
            "UPDATE issuer_state SET observed_time=? WHERE id=1",
            [clock(now)?],
        )
        .map_err(|_| Error::Database)?;
    if changed != 1 {
        return Err(Error::Database);
    }
    Ok(())
}
fn issue_roster(
    conn: &Connection,
    pin: &NativeRootPin,
    signer: &SigningKey,
    now: u64,
) -> Result<(i64, Vec<u8>)> {
    let previous: i64 = conn
        .query_row("SELECT generation FROM issuer_state WHERE id=1", [], |r| {
            r.get(0)
        })
        .map_err(|_| Error::Database)?;
    let next = previous.checked_add(1).ok_or(Error::Limit)?;
    let mut statement=conn.prepare("SELECT account,device,identity,key FROM members ORDER BY account COLLATE BINARY,device COLLATE BINARY").map_err(|_|Error::Database)?;
    let rows = statement
        .query_map([], |r| {
            Ok((
                r.get::<_, String>(0)?,
                r.get::<_, String>(1)?,
                r.get::<_, Vec<u8>>(2)?,
                r.get::<_, Vec<u8>>(3)?,
            ))
        })
        .map_err(|_| Error::Database)?;
    let mut records = Vec::new();
    for row in rows {
        let (a, d, i, k) = row.map_err(|_| Error::Database)?;
        if records.len() >= 128 {
            return Err(Error::Limit);
        }
        records.push(Value::Array(vec![
            Value::Text(a),
            Value::Text(d),
            Value::Bytes(i),
            Value::Bytes(k),
        ]));
    }
    drop(statement);
    let expires = now.checked_add(86400).ok_or(Error::Limit)?;
    let value = Value::Array(vec![
        Value::Text("MnemaTalk DeviceAuthorization".into()),
        Value::Integer(1.into()),
        Value::Text(pin.scope.origin().into()),
        Value::Text(pin.scope.community().into()),
        Value::Bytes(pin.group.clone()),
        Value::Integer((next as u64).into()),
        Value::Integer(now.into()),
        Value::Integer(expires.into()),
        Value::Array(records),
    ]);
    // Full roster may be larger than pairing wire; standard library signing here.
    use coset::{CoseSign1Builder, HeaderBuilder, TaggedCborSerializable, iana};
    use ed25519_dalek::Signer;
    let wire = CoseSign1Builder::new()
        .protected(
            HeaderBuilder::new()
                .algorithm(iana::Algorithm::Ed25519)
                .build(),
        )
        .payload(encode(&value)?)
        .create_signature(b"MnemaTalk DeviceAuthorization/v1", |t| {
            signer.sign(t).to_bytes().to_vec()
        })
        .build()
        .to_tagged_vec()
        .map_err(|_| Error::Invalid)?;
    let mut verifier = TrustVerifier::from_native_pin(
        pin.scope.clone(),
        &pin.authority,
        vec![
            NativeGroupFloor::from_native_store(&pin.group, previous as u64)
                .map_err(|_| Error::Trust)?,
        ],
        now,
    )
    .map_err(|_| Error::Trust)?;
    verifier
        .verify_roster(&wire, now)
        .map_err(|_| Error::Trust)?;
    let changed = conn
        .execute(
            "UPDATE issuer_state SET generation=? WHERE id=1 AND generation=?",
            params![next, previous],
        )
        .map_err(|_| Error::Database)?;
    if changed != 1 {
        return Err(Error::Database);
    }
    Ok((next, wire))
}
fn open(path: &Path, secrets: &impl NativeSecrets, create: bool) -> Result<Connection> {
    let key = secrets.read_seed("database-key")?;
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
    conn.execute_batch("PRAGMA cipher_memory_security=ON;PRAGMA journal_mode=DELETE;PRAGMA synchronous=EXTRA;PRAGMA fullfsync=ON;PRAGMA foreign_keys=ON;PRAGMA temp_store=MEMORY;PRAGMA busy_timeout=100;").map_err(|_|Error::Database)?;
    for (name, expected) in [
        ("synchronous", 3),
        ("fullfsync", 1),
        ("foreign_keys", 1),
        ("temp_store", 2),
        ("cipher_memory_security", 1),
        ("cipher_use_hmac", 1),
        ("cipher_plaintext_header_size", 0),
    ] {
        let value: rusqlite::types::Value = conn
            .pragma_query_value(None, name, |r| r.get(0))
            .map_err(|_| Error::Database)?;
        let number = match value {
            rusqlite::types::Value::Integer(n) => n,
            rusqlite::types::Value::Text(t) if t.len() < 20 => {
                t.parse().map_err(|_| Error::Database)?
            }
            _ => return Err(Error::Database),
        };
        if number != expected {
            return Err(Error::Database);
        }
    }
    Ok(conn)
}

#[cfg(test)]
fn fixture_exit(point: &str) {
    if std::env::var("MNEMA_ENROLLMENT_FAULT").as_deref() == Ok(point) {
        std::process::exit(73);
    }
}
