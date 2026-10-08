//! Ignored fresh-root/first-group proof. No data-send, root auto-trust,
//! restored-issuer activation, production admin-auth or SDK grant API.
use mnema_crypto_adapter_candidate::Scope;
use mnema_crypto_enrollment_prototype::{
    Device, Issuer, NativeAdmissionIntent, NativeRootPin, NativeSecrets,
};
use mnema_crypto_trust_candidate::{NativeGroupFloor, TrustVerifier};
use openmls::prelude::*;
use openmls_basic_credential::SignatureKeyPair;
use openmls_rust_crypto::RustCrypto;
use openmls_sqlite_storage::{Codec, SqliteStorageProvider};
use openmls_traits::OpenMlsProvider;
use rusqlite::{Connection, OpenFlags, TransactionBehavior, params};
use serde::{Serialize, de::DeserializeOwned};
use std::path::Path;
use zeroize::Zeroizing;
#[cfg(all(test, target_os = "macos"))]
mod native_fixture;
#[cfg(test)]
mod tests;

const SUITE: Ciphersuite = Ciphersuite::MLS_128_DHKEMX25519_CHACHA20POLY1305_SHA256_Ed25519;
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Error {
    Invalid,
    NativeUnavailable,
    Database,
    Provider,
    Trust,
    Incomplete,
    InspectionOnly,
}
pub type Result<T> = std::result::Result<T, Error>;
#[derive(Default)]
struct JsonCodec;
impl Codec for JsonCodec {
    type Error = serde_json::Error;
    fn to_vec<T: Serialize>(v: &T) -> std::result::Result<Vec<u8>, Self::Error> {
        serde_json::to_vec(v)
    }
    fn from_slice<T: DeserializeOwned>(v: &[u8]) -> std::result::Result<T, Self::Error> {
        serde_json::from_slice(v)
    }
}
struct Provider<'a> {
    crypto: &'a RustCrypto,
    storage: SqliteStorageProvider<JsonCodec, &'a Connection>,
}
impl<'a> OpenMlsProvider for Provider<'a> {
    type CryptoProvider = RustCrypto;
    type RandProvider = RustCrypto;
    type StorageProvider = SqliteStorageProvider<JsonCodec, &'a Connection>;
    fn crypto(&self) -> &Self::CryptoProvider {
        self.crypto
    }
    fn rand(&self) -> &Self::RandProvider {
        self.crypto
    }
    fn storage(&self) -> &Self::StorageProvider {
        &self.storage
    }
}

/// External native/admin workflow input. Shape is checked; this does NOT prove
/// actual authenticated admin custody or human OOB approval. Never deserialize.
pub struct NativeAdminSubject {
    account: String,
    device: String,
    identity: Vec<u8>,
}
impl NativeAdminSubject {
    pub fn from_native_admin_workflow(
        account: &str,
        device: &str,
        identity: &[u8],
    ) -> Result<Self> {
        for v in [account, device] {
            if v.is_empty()
                || v.len() > 128
                || !v
                    .bytes()
                    .all(|b| b.is_ascii_alphanumeric() || b"._-".contains(&b))
            {
                return Err(Error::Invalid);
            }
        }
        if identity.is_empty() || identity.len() > 256 {
            return Err(Error::Invalid);
        }
        Ok(Self {
            account: account.into(),
            device: device.into(),
            identity: identity.into(),
        })
    }
}
/// Public metadata only, for a future explicit out-of-band root ceremony.
/// No private material or live media/content capability is returned.
pub struct PublicBootstrap {
    pub origin: String,
    pub community: String,
    pub group: [u8; 32],
    pub authority: [u8; 32],
    pub fingerprint: [u8; 32],
    pub account: String,
    pub device: String,
    pub identity: Vec<u8>,
    pub device_key: [u8; 32],
    pub signed_roster: Vec<u8>,
    pub group_info: Vec<u8>,
}
pub struct FreshCommunity<'a, S: NativeSecrets> {
    issuer: Issuer<'a, S>,
    connection: Connection,
    public: PublicBootstrap,
    group: MlsGroup,
    signer: SignatureKeyPair,
    crypto: RustCrypto,
}
impl<'a, S: NativeSecrets> FreshCommunity<'a, S> {
    /// Caller must own a fresh reviewed native custody namespace and independent
    /// admin approval. This prototype deliberately exposes no IPC constructor.
    pub fn provision_first_group(
        path: &Path,
        secrets: &'a S,
        scope: Scope,
        subject: NativeAdminSubject,
        now: u64,
    ) -> Result<Self> {
        if now > i64::MAX as u64 {
            return Err(Error::Invalid);
        }
        let mut group = [0; 32];
        getrandom::fill(&mut group).map_err(|_| Error::NativeUnavailable)?;
        let mut issuer = Issuer::provision_fresh(path, secrets, scope.clone(), &group)
            .map_err(|_| Error::NativeUnavailable)?;
        let pin = issuer.pin_for_native_out_of_band_transfer();
        let device =
            Device::provision_native(secrets, pin.clone()).map_err(|_| Error::NativeUnavailable)?;
        let key = device.public_key();
        let intent = NativeAdmissionIntent::from_native_out_of_band_pin(
            &subject.account,
            &subject.device,
            &subject.identity,
            key,
        )
        .map_err(|_| Error::Invalid)?;
        let invitation = issuer
            .invite_native_reviewed_device(intent, now)
            .map_err(|_| Error::Trust)?;
        let proof = device.respond(&invitation, now).map_err(|_| Error::Trust)?;
        let signed_roster = issuer
            .admit_proven_device(&proof, now)
            .map_err(|_| Error::Trust)?;
        let mut verifier = TrustVerifier::from_native_pin(
            scope.clone(),
            &pin.authority_key(),
            vec![NativeGroupFloor::from_native_store(&group, 0).map_err(|_| Error::Trust)?],
            now,
        )
        .map_err(|_| Error::Trust)?;
        let roster = verifier
            .verify_roster(&signed_roster, now)
            .map_err(|_| Error::Trust)?;
        let approval = verifier
            .verify_device(&roster, &group, &subject.identity, &key, now)
            .map_err(|_| Error::Trust)?;
        if approval.account() != subject.account || approval.device() != subject.device {
            return Err(Error::Trust);
        }
        let seed = secrets
            .read_seed("device-key")
            .map_err(|_| Error::NativeUnavailable)?;
        let actual = ed25519_dalek::SigningKey::from_bytes(&seed)
            .verifying_key()
            .to_bytes();
        if actual != key {
            return Err(Error::Trust);
        }
        let signer =
            SignatureKeyPair::from_raw(SignatureScheme::ED25519, seed.to_vec(), key.to_vec());
        let mut connection = open(path, secrets)?;
        SqliteStorageProvider::<JsonCodec, _>::new(&mut connection)
            .run_migrations()
            .map_err(|_| Error::Database)?;
        connection.execute_batch("CREATE TABLE native_first_group(id INTEGER PRIMARY KEY CHECK(id=1),origin TEXT NOT NULL,community TEXT NOT NULL,group_id BLOB NOT NULL CHECK(length(group_id)=32),authority BLOB NOT NULL CHECK(length(authority)=32),account TEXT NOT NULL,device TEXT NOT NULL,identity BLOB NOT NULL CHECK(length(identity) BETWEEN 1 AND 256),device_key BLOB NOT NULL CHECK(length(device_key)=32),created INTEGER NOT NULL CHECK(created>=0));CREATE TABLE native_bootstrap_outbox(id INTEGER PRIMARY KEY CHECK(id=1),signed_roster BLOB NOT NULL CHECK(length(signed_roster) BETWEEN 1 AND 131072),group_info BLOB NOT NULL CHECK(length(group_info) BETWEEN 1 AND 32768));").map_err(|_|Error::Database)?;
        let crypto = RustCrypto::default();
        let tx = connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        let current:bool=tx.query_row("SELECT COUNT(*)=1 FROM issuer_state WHERE id=1 AND generation=1 AND observed_time=? AND origin=? AND community=? AND group_id=? AND authority=?",params![now as i64,scope.origin(),scope.community(),group.as_slice(),pin.authority_key().as_slice()],|r|r.get(0)).map_err(|_|Error::Database)?;
        if !current {
            return Err(Error::Trust);
        }
        let admitted:bool=tx.query_row("SELECT COUNT(*)=1 FROM members WHERE account=? AND device=? AND identity=? AND key=?",params![subject.account,subject.device,subject.identity,key.as_slice()],|r|r.get(0)).map_err(|_|Error::Database)?;
        let durable_roster: bool = tx
            .query_row(
                "SELECT COUNT(*)=1 FROM roster_outbox WHERE generation=1 AND wire=?",
                [signed_roster.as_slice()],
                |r| r.get(0),
            )
            .map_err(|_| Error::Database)?;
        if !admitted || !durable_roster {
            return Err(Error::Trust);
        }
        #[cfg(test)]
        fixture_fault(&tx, "before-provider")?;
        let provider = Provider {
            crypto: &crypto,
            storage: SqliteStorageProvider::new(&tx),
        };
        signer
            .store(provider.storage())
            .map_err(|_| Error::Database)?;
        let config = MlsGroupCreateConfig::builder()
            .ciphersuite(SUITE)
            .use_ratchet_tree_extension(true)
            .build();
        let first = MlsGroup::new_with_group_id(
            &provider,
            &signer,
            &config,
            GroupId::from_slice(&group),
            CredentialWithKey {
                credential: BasicCredential::new(subject.identity.clone()).into(),
                signature_key: key.to_vec().into(),
            },
        )
        .map_err(|e| match e {
            NewGroupError::StorageError(_) => Error::Database,
            _ => Error::Provider,
        })?;
        if first.epoch().as_u64() != 0 || first.members().count() != 1 {
            return Err(Error::Provider);
        }
        let group_info = first
            .export_group_info(provider.crypto(), &signer, true)
            .map_err(|_| Error::Provider)?
            .to_bytes()
            .map_err(|_| Error::Provider)?;
        persisted_fresh_provider(&tx, &provider, &first, &signer, &group_info)?;
        #[cfg(test)]
        fixture_fault(&tx, "before-record")?;
        one(tx
            .execute(
                "INSERT INTO native_first_group VALUES(1,?,?,?,?,?,?,?,?,?)",
                params![
                    scope.origin(),
                    scope.community(),
                    group.as_slice(),
                    pin.authority_key().as_slice(),
                    subject.account,
                    subject.device,
                    subject.identity,
                    key.as_slice(),
                    now as i64
                ],
            )
            .map_err(|_| Error::Database)?)?;
        #[cfg(test)]
        fixture_fault(&tx, "before-outbox")?;
        one(tx
            .execute(
                "INSERT INTO native_bootstrap_outbox VALUES(1,?,?)",
                params![signed_roster, group_info],
            )
            .map_err(|_| Error::Database)?)?;
        #[cfg(test)]
        fixture_exit("before-commit");
        tx.commit().map_err(|_| Error::Database)?;
        #[cfg(test)]
        fixture_exit("after-commit");
        let public = PublicBootstrap {
            origin: scope.origin().into(),
            community: scope.community().into(),
            group,
            authority: pin.authority_key(),
            fingerprint: pin.fingerprint().map_err(|_| Error::Trust)?,
            account: subject.account,
            device: subject.device,
            identity: subject.identity,
            device_key: key,
            signed_roster,
            group_info,
        };
        Ok(Self {
            issuer,
            connection,
            public,
            group: first,
            signer,
            crypto,
        })
    }
    /// Sealed transfer exists only for this actual newly created group, never
    /// for RestoredInspection. Caller must separately hold current native auth
    /// and native-modal/OOB approval before adopting or publishing.
    pub fn into_native_host(self) -> (Issuer<'a, S>, FreshGroupTransfer) {
        (
            self.issuer,
            FreshGroupTransfer {
                connection: self.connection,
                crypto: self.crypto,
                group: self.group,
                signer: self.signer,
                public: self.public,
            },
        )
    }
    pub fn public_for_native_oob_ceremony(&self) -> &PublicBootstrap {
        &self.public
    }
    /// Native issuer remains the reviewed separate authorization path. Adding a
    /// root-approved key here does not add it to MLS or mint any media capability.
    pub fn invite_native_reviewed_device(
        &mut self,
        intent: NativeAdmissionIntent,
        now: u64,
    ) -> Result<Vec<u8>> {
        self.issuer
            .invite_native_reviewed_device(intent, now)
            .map_err(|_| Error::Trust)
    }
    pub fn inspect_provider_epoch(&self) -> Result<u64> {
        let crypto = RustCrypto::default();
        let provider = Provider {
            crypto: &crypto,
            storage: SqliteStorageProvider::new(&self.connection),
        };
        let group = MlsGroup::load(provider.storage(), &GroupId::from_slice(&self.public.group))
            .map_err(|_| Error::Provider)?
            .ok_or(Error::Incomplete)?;
        Ok(group.epoch().as_u64())
    }
}
/// Not constructible/cloneable/deserializable; only an actual fresh factory can
/// produce this owner. It has no restored-state or renderer approval path.
pub struct FreshGroupTransfer {
    connection: Connection,
    crypto: RustCrypto,
    group: MlsGroup,
    signer: SignatureKeyPair,
    public: PublicBootstrap,
}
/// Native Rust integration only. Private seeds never enter any IPC record.
/// The consumer is responsible for immediate atomic Core adoption; extracting
/// these parts is not an authenticated admin or channel permission grant.
pub struct NativeHostParts {
    pub connection: Connection,
    pub crypto: RustCrypto,
    pub group: MlsGroup,
    pub signer: SignatureKeyPair,
    pub public: PublicBootstrap,
}
impl FreshGroupTransfer {
    pub fn consume_for_native_provider(self) -> NativeHostParts {
        NativeHostParts {
            connection: self.connection,
            crypto: self.crypto,
            group: self.group,
            signer: self.signer,
            public: self.public,
        }
    }
}
/// Restoring a complete or orphaned issuer database never returns FreshCommunity.
/// This inspector has no issuer/signer/group getter or application-send API.
pub struct RestoredInspection {
    public: PublicBootstrap,
}
impl RestoredInspection {
    pub fn load(
        path: &Path,
        secrets: &impl NativeSecrets,
        expected: NativeRootPin,
    ) -> Result<Self> {
        let connection = open(path, secrets)?;
        connection
            .pragma_update(None, "query_only", true)
            .map_err(|_| Error::Database)?;
        type Row = (
            String,
            String,
            Vec<u8>,
            Vec<u8>,
            String,
            String,
            Vec<u8>,
            Vec<u8>,
            Vec<u8>,
            Vec<u8>,
        );
        let row:Row=connection.query_row("SELECT p.origin,p.community,p.group_id,p.authority,p.account,p.device,p.identity,p.device_key,o.signed_roster,o.group_info FROM native_first_group p JOIN native_bootstrap_outbox o ON p.id=o.id WHERE p.id=1",[],|r|Ok((r.get(0)?,r.get(1)?,r.get(2)?,r.get(3)?,r.get(4)?,r.get(5)?,r.get(6)?,r.get(7)?,r.get(8)?,r.get(9)?))).map_err(|_|Error::Incomplete)?;
        let scope = Scope::new(&row.0, &row.1).map_err(|_| Error::Trust)?;
        let group: [u8; 32] = row.2.try_into().map_err(|_| Error::Trust)?;
        let authority: [u8; 32] = row.3.try_into().map_err(|_| Error::Trust)?;
        let stored =
            NativeRootPin::from_out_of_band(scope, &group, authority).map_err(|_| Error::Trust)?;
        let fingerprint = stored.fingerprint().map_err(|_| Error::Trust)?;
        if fingerprint != expected.fingerprint().map_err(|_| Error::Trust)? {
            return Err(Error::Trust);
        }
        if row.6.is_empty()
            || row.6.len() > 256
            || row.8.is_empty()
            || row.8.len() > 131072
            || row.9.is_empty()
            || row.9.len() > 32768
        {
            return Err(Error::Trust);
        }
        Ok(Self {
            public: PublicBootstrap {
                origin: row.0,
                community: row.1,
                group,
                authority,
                fingerprint,
                account: row.4,
                device: row.5,
                identity: row.6,
                device_key: row.7.try_into().map_err(|_| Error::Trust)?,
                signed_roster: row.8,
                group_info: row.9,
            },
        })
    }
    pub fn public_for_native_inspection(&self) -> &PublicBootstrap {
        &self.public
    }
    pub fn issue_or_send(&self) -> Result<()> {
        Err(Error::InspectionOnly)
    }
}
fn one(rows: usize) -> Result<()> {
    if rows == 1 {
        Ok(())
    } else {
        Err(Error::Database)
    }
}
/// The pinned official provider drops affected-row counts. Verify its actual
/// durable fresh-group records/readback within the same transaction, before
/// publishing any host record. This does not activate a restored group.
fn persisted_fresh_provider(
    conn: &Connection,
    provider: &Provider<'_>,
    fresh: &MlsGroup,
    signer: &SignatureKeyPair,
    group_info: &[u8],
) -> Result<()> {
    let restored = MlsGroup::load(provider.storage(), fresh.group_id())
        .map_err(|_| Error::Database)?
        .ok_or(Error::Database)?;
    if restored.epoch() != fresh.epoch()
        || restored.own_leaf_index() != fresh.own_leaf_index()
        || restored.members().count() != 1
        || !restored.is_active()
    {
        return Err(Error::Database);
    }
    let restored_info = restored
        .export_group_info(provider.crypto(), signer, true)
        .map_err(|_| Error::Database)?
        .to_bytes()
        .map_err(|_| Error::Database)?;
    if restored_info != group_info {
        return Err(Error::Database);
    }
    let old_export = Zeroizing::new(
        fresh
            .export_secret(
                provider.crypto(),
                "MnemaTalk first-group persistence probe",
                b"",
                32,
            )
            .map_err(|_| Error::Database)?,
    );
    let new_export = Zeroizing::new(
        restored
            .export_secret(
                provider.crypto(),
                "MnemaTalk first-group persistence probe",
                b"",
                32,
            )
            .map_err(|_| Error::Database)?,
    );
    if old_export.as_slice() != new_export.as_slice() {
        return Err(Error::Database);
    }
    let stored_signer = SignatureKeyPair::read(
        provider.storage(),
        &signer.to_public_vec(),
        SignatureScheme::ED25519,
    )
    .ok_or(Error::Database)?;
    let expected = Zeroizing::new(serde_json::to_vec(signer).map_err(|_| Error::Database)?);
    let actual = Zeroizing::new(serde_json::to_vec(&stored_signer).map_err(|_| Error::Database)?);
    if expected.as_slice() != actual.as_slice() {
        return Err(Error::Database);
    }
    let group_id = serde_json::to_vec(fresh.group_id()).map_err(|_| Error::Database)?;
    let epoch = serde_json::to_vec(&fresh.epoch()).map_err(|_| Error::Database)?;
    let pairs:i64=conn.query_row("SELECT COUNT(*) FROM openmls_epoch_keys_pairs WHERE group_id=? AND epoch_id=? AND leaf_index=? AND provider_version=1 AND length(key_pairs) BETWEEN 1 AND 32768",params![group_id,epoch,fresh.own_leaf_index().u32()],|r|r.get(0)).map_err(|_|Error::Database)?;
    if pairs != 1 {
        return Err(Error::Database);
    }
    Ok(())
}
fn open(path: &Path, secrets: &impl NativeSecrets) -> Result<Connection> {
    let meta = std::fs::symlink_metadata(path).map_err(|_| Error::Database)?;
    if !meta.is_file() || meta.len() > 64 * 1024 * 1024 {
        return Err(Error::Invalid);
    }
    let key = secrets
        .read_seed("database-key")
        .map_err(|_| Error::NativeUnavailable)?;
    let conn = Connection::open_with_flags(
        path,
        OpenFlags::SQLITE_OPEN_READ_WRITE
            | OpenFlags::SQLITE_OPEN_NO_MUTEX
            | OpenFlags::SQLITE_OPEN_NOFOLLOW,
    )
    .map_err(|_| Error::Database)?;
    let mut raw = Zeroizing::new(String::from("x'"));
    use std::fmt::Write;
    for b in key.iter() {
        write!(&mut *raw, "{b:02x}").map_err(|_| Error::Invalid)?;
    }
    raw.push('\'');
    conn.pragma_update(None, "key", raw.as_str())
        .map_err(|_| Error::Database)?;
    conn.query_row("SELECT COUNT(*) FROM sqlite_master", [], |r| {
        r.get::<_, i64>(0)
    })
    .map_err(|_| Error::Database)?;
    let version: String = conn
        .pragma_query_value(None, "cipher_version", |r| r.get(0))
        .map_err(|_| Error::Database)?;
    if !version.starts_with("4.") {
        return Err(Error::Database);
    }
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
        let n = match value {
            rusqlite::types::Value::Integer(n) => n,
            rusqlite::types::Value::Text(t) if t.len() < 20 => {
                t.parse().map_err(|_| Error::Database)?
            }
            _ => return Err(Error::Database),
        };
        if n != expected {
            return Err(Error::Database);
        }
    }
    Ok(conn)
}
#[cfg(test)]
fn fixture_fault(tx: &Connection, point: &str) -> Result<()> {
    if point == "before-provider"
        && let Ok(fault) = std::env::var("MNEMA_FIRST_GROUP_FAULT")
        && let Some(slot) = fault.strip_prefix("ignore-provider-")
    {
        let sql=match slot {
                    "signature"=>"CREATE TEMP TRIGGER suppress_provider BEFORE INSERT ON openmls_signature_keys BEGIN SELECT RAISE(IGNORE);END;".to_owned(),
                    "epoch-keys"=>"CREATE TEMP TRIGGER suppress_provider BEFORE INSERT ON openmls_epoch_keys_pairs BEGIN SELECT RAISE(IGNORE);END;".to_owned(),
                    v if ["join_group_config","tree","interim_transcript_hash","context","confirmation_tag","group_state","message_secrets","resumption_psk_store","own_leaf_index","group_epoch_secrets"].contains(&v)=>format!("CREATE TEMP TRIGGER suppress_provider BEFORE INSERT ON openmls_group_data WHEN NEW.data_type='{v}' BEGIN SELECT RAISE(IGNORE);END;"),
                    _=>return Err(Error::Invalid),
                };
        conn_execute_fixture(tx, &sql)?;
    }
    match std::env::var("MNEMA_FIRST_GROUP_FAULT").as_deref() {
        Ok("ignore-record") if point=="before-record"=>tx.execute_batch("CREATE TEMP TRIGGER suppress_record BEFORE INSERT ON native_first_group BEGIN SELECT RAISE(IGNORE);END;").map_err(|_|Error::Database)?,
        Ok("ignore-outbox") if point=="before-outbox"=>tx.execute_batch("CREATE TEMP TRIGGER suppress_outbox BEFORE INSERT ON native_bootstrap_outbox BEGIN SELECT RAISE(IGNORE);END;").map_err(|_|Error::Database)?,
        _=>{}
    }
    Ok(())
}
#[cfg(test)]
fn fixture_exit(point: &str) {
    if std::env::var("MNEMA_FIRST_GROUP_FAULT").as_deref() == Ok(point) {
        std::process::exit(73)
    }
}

#[cfg(test)]
fn conn_execute_fixture(tx: &Connection, sql: &str) -> Result<()> {
    tx.execute_batch(sql).map_err(|_| Error::Database)
}
