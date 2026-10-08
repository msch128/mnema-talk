//! Community credentials are root-signed application policy, not Voice grants.
//! Every activation still needs the actual native Voice routing lease and a
//! separate fresh MLS group. Root bootstrap is the existing native pinned SDK.
use super::*;
use coset::{CoseSign1Builder, Header, HeaderBuilder, TaggedCborSerializable, iana};
use ed25519_dalek::{Signature, Signer, SigningKey, VerifyingKey};
use sha2::{Digest, Sha256};
use std::collections::HashSet;

const DOMAIN: &str = "MnemaTalk CommunityAuthorization";
const AAD: &[u8] = b"MnemaTalk CommunityAuthorization/v1";
const GROUP_DOMAIN: &str = "MnemaTalk VoiceGroupCreation";
const GROUP_AAD: &[u8] = b"MnemaTalk VoiceGroupCreation/v1";
const CAPABILITY: &str = "voice-group-v1";
const HEADER: &[u8] = &[0xa1, 0x01, 0x32];
const MAX_WIRE: usize = 32768;
const MAX_DEVICES: usize = 128;
const MAX_LIFETIME: u64 = 86400;
const MAX_CREATION_LIFETIME: u64 = 300;
type StoredCommunityRow = (Vec<u8>, Vec<u8>, Vec<u8>, i64);
const TABLE: &str = "CREATE TABLE IF NOT EXISTS sdk_community_authorization(id INTEGER PRIMARY KEY CHECK(id=1),generation BLOB NOT NULL CHECK(length(generation)=8),digest BLOB NOT NULL CHECK(length(digest)=32),wire BLOB NOT NULL CHECK(length(wire) BETWEEN 1 AND 32768),observed_time INTEGER NOT NULL CHECK(observed_time>=0));";

#[derive(Clone)]
struct DeviceRecord {
    account: String,
    device: String,
    identity: Vec<u8>,
    key: [u8; 32],
}
/// Actual pinned origin/community/root from an already live native SDK. No
/// public constructor, root fetch, Deserialize or server role conversion.
pub struct NativeCommunityAnchor {
    owner: u64,
    scope: Scope,
    root: [u8; 32],
}
impl NativeCommunityAnchor {
    pub fn origin(&self) -> &str {
        self.scope.origin()
    }
    pub fn community(&self) -> &str {
        self.scope.community()
    }
    pub fn root_public_key(&self) -> &[u8; 32] {
        &self.root
    }
}
/// Authenticated root policy and exact bytes; no current Voice permission.
/// Native owner must revalidate its durable floor/expiry on every use.
pub struct VerifiedCommunityAuthorization {
    owner: u64,
    scope: Scope,
    root: [u8; 32],
    generation: u64,
    issued: u64,
    expires: u64,
    digest: [u8; 32],
    devices: Vec<DeviceRecord>,
}
impl VerifiedCommunityAuthorization {
    pub fn generation(&self) -> u64 {
        self.generation
    }
    pub fn expires_at(&self) -> u64 {
        self.expires
    }
    pub fn digest(&self) -> &[u8; 32] {
        &self.digest
    }
    pub fn device_count(&self) -> usize {
        self.devices.len()
    }
    pub fn origin(&self) -> &str {
        self.scope.origin()
    }
    pub fn community(&self) -> &str {
        self.scope.community()
    }
    fn device(&self, account: &str, device: &str) -> Result<&DeviceRecord> {
        self.devices
            .iter()
            .find(|r| r.account == account && r.device == device)
            .ok_or(Error::Unauthorized)
    }
}
/// Verified creator proposal. It does not authenticate current Voice routing,
/// roster consistency, actual MLS GroupInfo or group membership.
pub struct VerifiedVoiceCreation {
    authorization_generation: u64,
    channel: String,
    room: String,
    group: [u8; 32],
    account: String,
    device: String,
    expires: u64,
}
impl VerifiedVoiceCreation {
    pub fn channel(&self) -> &str {
        &self.channel
    }
    pub fn room_incarnation(&self) -> &str {
        &self.room
    }
    pub fn group(&self) -> &[u8; 32] {
        &self.group
    }
    pub fn creator_account(&self) -> &str {
        &self.account
    }
    pub fn creator_device(&self) -> &str {
        &self.device
    }
    pub fn authorization_generation(&self) -> u64 {
        self.authorization_generation
    }
    pub fn expires_at(&self) -> u64 {
        self.expires
    }
}
impl Sdk {
    pub fn native_community_anchor(&self, now: u64) -> Result<NativeCommunityAnchor> {
        self.current(now)?;
        Ok(NativeCommunityAnchor {
            owner: self.core.owner,
            scope: self.core.pin.scope.clone(),
            root: self.core.pin.authority,
        })
    }
    /// Fixed native root issuance from exact already-approved admission records.
    /// Caller must hold widened native OS community ceremony/current admin scope;
    /// this function never accepts a requested device/key/capability/lifetime.
    pub fn issue_native_community_authorization(
        &mut self,
        secrets: &impl NativeSecrets,
        now: u64,
    ) -> Result<Vec<u8>> {
        if let Err(error) = self.current(now) {
            self.retire_native();
            return Err(error);
        }
        let seed = match secrets.read_seed("issuer-root") {
            Ok(seed) => seed,
            Err(_) => {
                self.retire_native();
                return Err(Error::KeyUnavailable);
            }
        };
        let signer = SigningKey::from_bytes(&seed);
        if signer.verifying_key().to_bytes() != self.core.pin.authority {
            self.retire_native();
            return Err(Error::Unauthorized);
        }
        let anchor = self.native_community_anchor(now)?;
        let result = (|| {
            let tx = self
                .core
                .connection
                .transaction_with_behavior(TransactionBehavior::Immediate)
                .map_err(|_| Error::Database)?;
            let own_host:bool=tx.query_row("SELECT COUNT(*)=1 FROM native_first_group WHERE id=1 AND origin=? AND community=? AND group_id=? AND authority=? AND account=? AND device=?",params![anchor.scope.origin(),anchor.scope.community(),self.core.pin.group,anchor.root.as_slice(),self.binding.account,self.binding.device],|r|r.get(0)).map_err(|_|Error::Unauthorized)?;
            if !own_host {
                return Err(Error::Unauthorized);
            }
            let (mut old_trust, old_roster, generation) = current_trust(&tx, &self.core.pin, now)?;
            let old: Vec<u8> = tx
                .query_row("SELECT roster FROM core_state WHERE id=1", [], |r| r.get(0))
                .map_err(|_| Error::Database)?;
            let old_envelope = CoseSign1::from_tagged_slice(&old).map_err(|_| Error::Invalid)?;
            let payload = old_envelope.payload.ok_or(Error::Invalid)?;
            let parsed = decode(&payload)?;
            let fields = array(&parsed, 9)?;
            if text(&fields[0])? != "MnemaTalk DeviceAuthorization"
                || integer(&fields[1])? != 1
                || text(&fields[2])? != anchor.scope.origin()
                || text(&fields[3])? != anchor.scope.community()
                || bytes(&fields[4])? != self.core.pin.group
                || integer(&fields[5])? != generation
            {
                return Err(Error::Trust);
            }
            let issued = integer(&fields[6])?;
            let expires = integer(&fields[7])?;
            let Value::Array(entries) = &fields[8] else {
                return Err(Error::Invalid);
            };
            let mut records = Vec::with_capacity(entries.len());
            for entry in entries {
                let row = array(entry, 4)?;
                let account = text(&row[0])?;
                let device = text(&row[1])?;
                let identity = bytes(&row[2])?;
                let key = bytes(&row[3])?;
                let record = checked_device(account, device, identity, key)?;
                let verified = old_trust
                    .verify_device(&old_roster, &self.core.pin.group, identity, key, now)
                    .map_err(|_| Error::Unauthorized)?;
                if verified.account() != account || verified.device() != device {
                    return Err(Error::Unauthorized);
                }
                records.push(record);
            }
            validate_records(&records)?;
            let body = Value::Array(vec![
                Value::Text(DOMAIN.into()),
                Value::Integer(1.into()),
                Value::Text(anchor.scope.origin().into()),
                Value::Text(anchor.scope.community().into()),
                Value::Integer(generation.into()),
                Value::Integer(issued.into()),
                Value::Integer(expires.into()),
                Value::Array(
                    records
                        .iter()
                        .map(|r| {
                            Value::Array(vec![
                                Value::Text(r.account.clone()),
                                Value::Text(r.device.clone()),
                                Value::Bytes(r.identity.clone()),
                                Value::Bytes(r.key.to_vec()),
                                Value::Text(CAPABILITY.into()),
                            ])
                        })
                        .collect(),
                ),
            ]);
            let wire = CoseSign1Builder::new()
                .protected(
                    HeaderBuilder::new()
                        .algorithm(iana::Algorithm::Ed25519)
                        .build(),
                )
                .payload(encode(&body)?)
                .create_signature(AAD, |transcript| {
                    signer.sign(transcript).to_bytes().to_vec()
                })
                .build()
                .to_tagged_vec()
                .map_err(|_| Error::Invalid)?;
            let verified = verify_document(&anchor, &wire, now)?;
            persist(&tx, &wire, &verified, now)?;
            tx.commit().map_err(|_| Error::UnknownCommit)?;
            Ok(wire)
        })();
        if result.is_err() {
            self.retire_native()
        }
        result
    }
    /// Native durable acceptance against the already pinned root. No approval
    /// comes from HTTP role/JSON. Exact floor/bytes/time persist before return.
    pub fn install_native_community_authorization(
        &mut self,
        wire: &[u8],
        now: u64,
    ) -> Result<VerifiedCommunityAuthorization> {
        let anchor = match self.native_community_anchor(now) {
            Ok(anchor) => anchor,
            Err(error) => {
                self.retire_native();
                return Err(error);
            }
        };
        let own = self
            .core
            .group
            .as_ref()
            .and_then(|g| g.own_leaf())
            .ok_or(Error::WrongPhase)?;
        let own_identity = own.credential().serialized_content().to_vec();
        let own_key = own.signature_key().as_slice().to_vec();
        let result = (|| {
            let verified = verify_document(&anchor, wire, now)?;
            let tx = self
                .core
                .connection
                .transaction_with_behavior(TransactionBehavior::Immediate)
                .map_err(|_| Error::Database)?;
            persist(&tx, wire, &verified, now)?;
            tx.commit().map_err(|_| Error::UnknownCommit)?;
            if !verified
                .devices
                .iter()
                .any(|r| r.identity == own_identity && r.key.as_slice() == own_key)
            {
                return Err(Error::Unauthorized);
            }
            Ok(verified)
        })();
        if result.is_ok() {
            self.bridges.clear();
        }
        if result.is_err() {
            self.retire_native()
        }
        result
    }
    /// Reverify the same live native owner/root, durable current generation and
    /// exact certificate reference before using a verified creator proposal.
    pub fn verify_native_voice_creation(
        &mut self,
        authorization: &VerifiedCommunityAuthorization,
        wire: &[u8],
        now: u64,
    ) -> Result<VerifiedVoiceCreation> {
        let result = (|| {
            self.current(now)?;
            let tx = self
                .core
                .connection
                .transaction_with_behavior(TransactionBehavior::Immediate)
                .map_err(|_| Error::Database)?;
            current_authorization(&tx, self.core.owner, &self.core.pin, authorization, now)?;
            let verified = verify_creation(authorization, wire, now)?;
            require_one(
                tx.execute(
                    "UPDATE sdk_community_authorization SET observed_time=? WHERE id=1",
                    [clock(now)?],
                )
                .map_err(|_| Error::Database)?,
            )?;
            let exact:bool=tx.query_row("SELECT COUNT(*)=1 FROM sdk_community_authorization WHERE id=1 AND observed_time=?",[clock(now)?],|r|r.get(0)).map_err(|_|Error::Database)?;
            if !exact {
                return Err(Error::Database);
            }
            tx.commit().map_err(|_| Error::UnknownCommit)?;
            Ok(verified)
        })();
        if result.is_err() {
            self.retire_native()
        }
        result
    }
}
fn verify_creation(
    authorization: &VerifiedCommunityAuthorization,
    wire: &[u8],
    now: u64,
) -> Result<VerifiedVoiceCreation> {
    let (signed, payload) = cose_parts(wire)?;
    let decoded = decode(&payload)?;
    let fields = array(&decoded, 13)?;
    if text(&fields[0])? != GROUP_DOMAIN || integer(&fields[1])? != 1 {
        return Err(Error::Invalid);
    }
    if text(&fields[2])? != authorization.scope.origin()
        || text(&fields[3])? != authorization.scope.community()
    {
        return Err(Error::Unauthorized);
    }
    let channel = text(&fields[4])?;
    uuid(channel)?;
    let room = text(&fields[5])?;
    uuid(room)?;
    let group: [u8; 32] = bytes(&fields[6])?.try_into().map_err(|_| Error::Invalid)?;
    if group == [0; 32] {
        return Err(Error::Invalid);
    }
    let account = text(&fields[7])?;
    uuid(account)?;
    let device = text(&fields[8])?;
    uuid(device)?;
    if integer(&fields[9])? != authorization.generation
        || bytes(&fields[10])? != authorization.digest
    {
        return Err(Error::Unauthorized);
    }
    let issued = integer(&fields[11])?;
    let expires = integer(&fields[12])?;
    check_time(issued, expires, now, MAX_CREATION_LIFETIME)?;
    if expires > authorization.expires {
        return Err(Error::Unauthorized);
    }
    let record = authorization.device(account, device)?;
    verify_signature(&signed, GROUP_AAD, &record.key)?;
    Ok(VerifiedVoiceCreation {
        authorization_generation: authorization.generation,
        channel: channel.into(),
        room: room.into(),
        group,
        account: account.into(),
        device: device.into(),
        expires,
    })
}
fn current_authorization(
    conn: &Connection,
    owner: u64,
    pin: &NativeBootstrap,
    authorization: &VerifiedCommunityAuthorization,
    now: u64,
) -> Result<()> {
    if authorization.owner != owner
        || authorization.scope != pin.scope
        || authorization.root != pin.authority
    {
        return Err(Error::Stale);
    }
    check_time(
        authorization.issued,
        authorization.expires,
        now,
        MAX_LIFETIME,
    )?;
    let stored: (Vec<u8>, Vec<u8>, i64) = conn
        .query_row(
            "SELECT generation,digest,observed_time FROM sdk_community_authorization WHERE id=1",
            [],
            |r| Ok((r.get(0)?, r.get(1)?, r.get(2)?)),
        )
        .map_err(|_| Error::Database)?;
    if stored.0 != authorization.generation.to_be_bytes()
        || stored.1 != authorization.digest
        || stored.2 > clock(now)?
    {
        return Err(Error::Stale);
    }
    Ok(())
}
fn persist(
    conn: &Connection,
    wire: &[u8],
    verified: &VerifiedCommunityAuthorization,
    now: u64,
) -> Result<()> {
    conn.execute_batch(TABLE).map_err(|_| Error::Database)?;
    let old:Option<StoredCommunityRow>=conn.query_row("SELECT generation,digest,wire,observed_time FROM sdk_community_authorization WHERE id=1",[],|r|Ok((r.get(0)?,r.get(1)?,r.get(2)?,r.get(3)?))).optional().map_err(|_|Error::Database)?;
    if let Some((generation, digest, stored, last)) = old {
        let generation = u64::from_be_bytes(generation.try_into().map_err(|_| Error::Database)?);
        if clock(now)? < last || verified.generation < generation {
            return Err(Error::Trust);
        }
        if verified.generation == generation && (digest != verified.digest || stored != wire) {
            return Err(Error::Replay);
        }
        require_one(conn.execute("UPDATE sdk_community_authorization SET generation=?,digest=?,wire=?,observed_time=? WHERE id=1",params![verified.generation.to_be_bytes().as_slice(),verified.digest.as_slice(),wire,clock(now)?]).map_err(|_|Error::Database)?)?;
    } else {
        require_one(
            conn.execute(
                "INSERT INTO sdk_community_authorization VALUES(1,?,?,?,?)",
                params![
                    verified.generation.to_be_bytes().as_slice(),
                    verified.digest.as_slice(),
                    wire,
                    clock(now)?
                ],
            )
            .map_err(|_| Error::Database)?,
        )?;
    }
    let exact:bool=conn.query_row("SELECT COUNT(*)=1 FROM sdk_community_authorization WHERE id=1 AND generation=? AND digest=? AND wire=? AND observed_time=?",params![verified.generation.to_be_bytes().as_slice(),verified.digest.as_slice(),wire,clock(now)?],|r|r.get(0)).map_err(|_|Error::Database)?;
    if !exact {
        return Err(Error::Database);
    }
    Ok(())
}
fn verify_document(
    anchor: &NativeCommunityAnchor,
    wire: &[u8],
    now: u64,
) -> Result<VerifiedCommunityAuthorization> {
    let (signed, payload) = cose_parts(wire)?;
    verify_signature(&signed, AAD, &anchor.root)?;
    let parsed = decode(&payload)?;
    let fields = array(&parsed, 8)?;
    if text(&fields[0])? != DOMAIN || integer(&fields[1])? != 1 {
        return Err(Error::Invalid);
    }
    if text(&fields[2])? != anchor.scope.origin() || text(&fields[3])? != anchor.scope.community() {
        return Err(Error::Unauthorized);
    }
    let generation = integer(&fields[4])?;
    if generation == 0 {
        return Err(Error::Invalid);
    }
    let issued = integer(&fields[5])?;
    let expires = integer(&fields[6])?;
    check_time(issued, expires, now, MAX_LIFETIME)?;
    let Value::Array(entries) = &fields[7] else {
        return Err(Error::Invalid);
    };
    if entries.is_empty() || entries.len() > MAX_DEVICES {
        return Err(Error::Limit);
    }
    let mut devices = Vec::with_capacity(entries.len());
    for entry in entries {
        let row = array(entry, 5)?;
        if text(&row[4])? != CAPABILITY {
            return Err(Error::Unauthorized);
        }
        devices.push(checked_device(
            text(&row[0])?,
            text(&row[1])?,
            bytes(&row[2])?,
            bytes(&row[3])?,
        )?);
    }
    validate_records(&devices)?;
    Ok(VerifiedCommunityAuthorization {
        owner: anchor.owner,
        scope: anchor.scope.clone(),
        root: anchor.root,
        generation,
        issued,
        expires,
        digest: Sha256::digest(wire).into(),
        devices,
    })
}
fn checked_device(
    account: &str,
    device: &str,
    identity: &[u8],
    key: &[u8],
) -> Result<DeviceRecord> {
    uuid(account)?;
    uuid(device)?;
    if identity != format!("{account}.{device}").as_bytes() {
        return Err(Error::Unauthorized);
    }
    let key: [u8; 32] = key.try_into().map_err(|_| Error::Invalid)?;
    let public = VerifyingKey::from_bytes(&key).map_err(|_| Error::Invalid)?;
    if public.is_weak() {
        return Err(Error::Unauthorized);
    }
    Ok(DeviceRecord {
        account: account.into(),
        device: device.into(),
        identity: identity.to_vec(),
        key,
    })
}
fn validate_records(records: &[DeviceRecord]) -> Result<()> {
    if records.is_empty() || records.len() > MAX_DEVICES {
        return Err(Error::Limit);
    }
    let mut previous: Option<(&str, &str)> = None;
    let mut keys = HashSet::new();
    for r in records {
        let current = (r.account.as_str(), r.device.as_str());
        if previous.is_some_and(|p| p >= current) || !keys.insert(r.key) {
            return Err(Error::Unauthorized);
        }
        previous = Some(current);
    }
    Ok(())
}
fn check_time(issued: u64, expires: u64, now: u64, max_lifetime: u64) -> Result<()> {
    clock(now)?;
    clock(issued)?;
    clock(expires)?;
    if issued > now || expires <= issued || expires - issued > max_lifetime || now >= expires {
        return Err(Error::Trust);
    }
    Ok(())
}
fn verify_signature(signed: &CoseSign1, aad: &[u8], key: &[u8; 32]) -> Result<()> {
    let key = VerifyingKey::from_bytes(key).map_err(|_| Error::Trust)?;
    if key.is_weak() {
        return Err(Error::Trust);
    }
    signed.verify_signature(aad, |signature, transcript| {
        let signature = Signature::from_slice(signature).map_err(|_| Error::Trust)?;
        key.verify_strict(transcript, &signature)
            .map_err(|_| Error::Trust)
    })
}
fn cose_parts(wire: &[u8]) -> Result<(CoseSign1, Vec<u8>)> {
    let parsed = decode(wire)?;
    let Value::Tag(18, body) = &parsed else {
        return Err(Error::Invalid);
    };
    let fields = array(body, 4)?;
    if bytes(&fields[0])? != HEADER
        || !matches!(&fields[1],Value::Map(m)if m.is_empty())
        || bytes(&fields[3])?.len() != 64
    {
        return Err(Error::Invalid);
    }
    let signed = CoseSign1::from_tagged_slice(wire).map_err(|_| Error::Invalid)?;
    if signed.unprotected != Header::default() {
        return Err(Error::Invalid);
    }
    let payload = signed.payload.clone().ok_or(Error::Invalid)?;
    Ok((signed, payload))
}
fn array(v: &Value, size: usize) -> Result<&[Value]> {
    match v {
        Value::Array(a) if a.len() == size => Ok(a),
        _ => Err(Error::Invalid),
    }
}
fn text(v: &Value) -> Result<&str> {
    match v {
        Value::Text(s) => Ok(s),
        _ => Err(Error::Invalid),
    }
}
fn bytes(v: &Value) -> Result<&[u8]> {
    match v {
        Value::Bytes(b) => Ok(b),
        _ => Err(Error::Invalid),
    }
}
fn integer(v: &Value) -> Result<u64> {
    match v {
        Value::Integer(i) => u64::try_from(*i).map_err(|_| Error::Invalid),
        _ => Err(Error::Invalid),
    }
}
fn decode(wire: &[u8]) -> Result<Value> {
    preflight(wire)?;
    let value: Value = coset::cbor::de::from_reader(wire).map_err(|_| Error::Invalid)?;
    if encode(&value)? != wire {
        return Err(Error::Invalid);
    }
    Ok(value)
}
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
        if depth > 5 || nodes >= 2048 || remaining > 2048 {
            return Err(Error::Limit);
        }
        stack.push((remaining - 1, depth));
        nodes += 1;
        let byte = *wire.get(index).ok_or(Error::Invalid)?;
        index += 1;
        let major = byte >> 5;
        let ai = byte & 31;
        let argument = match ai {
            0..=23 => u64::from(ai),
            24..=27 => {
                let width = 1usize << (ai - 24);
                let end = index.checked_add(width).ok_or(Error::Limit)?;
                let data = wire.get(index..end).ok_or(Error::Invalid)?;
                let mut value = 0u64;
                for b in data {
                    value = (value << 8) | u64::from(*b)
                }
                let minimum = match width {
                    1 => 24,
                    2 => 256,
                    4 => 65536,
                    _ => 1u64 << 32,
                };
                if value < minimum {
                    return Err(Error::Invalid);
                }
                index = end;
                value
            }
            _ => return Err(Error::Invalid),
        };
        match major {
            0 | 1 => (),
            2 | 3 => {
                let count = usize::try_from(argument).map_err(|_| Error::Limit)?;
                index = index.checked_add(count).ok_or(Error::Limit)?;
                if index > wire.len() {
                    return Err(Error::Invalid);
                }
            }
            4 => stack.push((argument, depth + 1)),
            5 => stack.push((argument.checked_mul(2).ok_or(Error::Limit)?, depth + 1)),
            6 => stack.push((1, depth + 1)),
            _ => return Err(Error::Invalid),
        }
    }
    if index != wire.len() {
        return Err(Error::Invalid);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    const ACCOUNT: &str = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
    const DEVICE: &str = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
    const NOW: u64 = 1000;
    fn root() -> SigningKey {
        SigningKey::from_bytes(&[19; 32])
    }
    fn device_key() -> SigningKey {
        SigningKey::from_bytes(&[27; 32])
    }
    fn anchor() -> NativeCommunityAnchor {
        NativeCommunityAnchor {
            owner: 1,
            scope: Scope::new("https://community.example", "community").unwrap(),
            root: root().verifying_key().to_bytes(),
        }
    }
    fn payload() -> Value {
        Value::Array(vec![
            Value::Text(DOMAIN.into()),
            Value::Integer(1.into()),
            Value::Text(anchor().origin().into()),
            Value::Text(anchor().community().into()),
            Value::Integer(1.into()),
            Value::Integer(NOW.into()),
            Value::Integer((NOW + 1000).into()),
            Value::Array(vec![Value::Array(vec![
                Value::Text(ACCOUNT.into()),
                Value::Text(DEVICE.into()),
                Value::Bytes(format!("{ACCOUNT}.{DEVICE}").into_bytes()),
                Value::Bytes(device_key().verifying_key().to_bytes().to_vec()),
                Value::Text(CAPABILITY.into()),
            ])]),
        ])
    }
    fn sign(value: &Value, signer: &SigningKey, aad: &[u8]) -> Vec<u8> {
        CoseSign1Builder::new()
            .protected(
                HeaderBuilder::new()
                    .algorithm(iana::Algorithm::Ed25519)
                    .build(),
            )
            .payload(encode(value).unwrap())
            .create_signature(aad, |t| signer.sign(t).to_bytes().to_vec())
            .build()
            .to_tagged_vec()
            .unwrap()
    }
    fn proposal(authorization: &VerifiedCommunityAuthorization) -> Value {
        Value::Array(vec![
            Value::Text(GROUP_DOMAIN.into()),
            Value::Integer(1.into()),
            Value::Text(anchor().origin().into()),
            Value::Text(anchor().community().into()),
            Value::Text("cccccccc-cccc-4ccc-8ccc-cccccccccccc".into()),
            Value::Text("dddddddd-dddd-4ddd-8ddd-dddddddddddd".into()),
            Value::Bytes(vec![77; 32]),
            Value::Text(ACCOUNT.into()),
            Value::Text(DEVICE.into()),
            Value::Integer(authorization.generation.into()),
            Value::Bytes(authorization.digest.to_vec()),
            Value::Integer(NOW.into()),
            Value::Integer((NOW + 100).into()),
        ])
    }
    #[test]
    fn signed_community_exact_profile_and_creator_device_key_references() {
        let wire = sign(&payload(), &root(), AAD);
        let verified = verify_document(&anchor(), &wire, NOW).unwrap();
        assert_eq!(verified.device_count(), 1);
        assert_eq!(verified.generation(), 1);
        let wire = sign(&proposal(&verified), &device_key(), GROUP_AAD);
        let creation = verify_creation(&verified, &wire, NOW).unwrap();
        assert_eq!(creation.creator_account(), ACCOUNT);
        assert_eq!(creation.creator_device(), DEVICE);
        assert_eq!(creation.group(), &[77; 32]);
        assert!(
            verify_creation(
                &verified,
                &sign(&proposal(&verified), &root(), GROUP_AAD),
                NOW
            )
            .is_err()
        );
    }
    #[test]
    fn root_wrong_scope_domain_capability_and_identity_reject_even_with_valid_signature() {
        for fault in 0..9 {
            let mut value = payload();
            let Value::Array(fields) = &mut value else {
                panic!("fixture")
            };
            match fault {
                0 => fields[0] = Value::Text("MnemaTalk DeviceAuthorization".into()),
                1 => fields[1] = Value::Integer(2.into()),
                2 => fields[2] = Value::Text("https://other.example".into()),
                3 => fields[3] = Value::Text("other-community".into()),
                4 => fields[4] = Value::Integer(0.into()),
                5 => fields[5] = Value::Integer((NOW + 1).into()),
                6 => fields[6] = Value::Integer((NOW + MAX_LIFETIME + 1).into()),
                n => {
                    let Value::Array(records) = &mut fields[7] else {
                        panic!("fixture")
                    };
                    let Value::Array(record) = &mut records[0] else {
                        panic!("fixture")
                    };
                    record[if n == 7 { 2 } else { 4 }] = if n == 7 {
                        Value::Bytes(b"spoofed identity".to_vec())
                    } else {
                        Value::Text("admin".into())
                    };
                }
            }
            assert!(
                verify_document(&anchor(), &sign(&value, &root(), AAD), NOW).is_err(),
                "fixed fault {fault}"
            );
        }
    }
    #[test]
    fn community_duplicate_unsorted_devices_weak_key_and_nil_uuid_are_denied() {
        for fault in 0..5 {
            let mut value = payload();
            let Value::Array(fields) = &mut value else {
                panic!("fixture")
            };
            let Value::Array(records) = &mut fields[7] else {
                panic!("fixture")
            };
            match fault {
                0 => records.push(records[0].clone()),
                1 => {
                    let mut second = records[0].clone();
                    let Value::Array(record) = &mut second else {
                        panic!("fixture")
                    };
                    record[1] = Value::Text("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa".into());
                    record[2] = Value::Bytes(
                        format!("{ACCOUNT}.aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa").into_bytes(),
                    );
                    record[3] = Value::Bytes(
                        SigningKey::from_bytes(&[49; 32])
                            .verifying_key()
                            .to_bytes()
                            .to_vec(),
                    );
                    records.push(second);
                }
                2 => {
                    let Value::Array(record) = &mut records[0] else {
                        panic!("fixture")
                    };
                    record[3] = Value::Bytes({
                        let mut weak = vec![0; 32];
                        weak[0] = 1;
                        weak
                    });
                }
                3 => {
                    let Value::Array(record) = &mut records[0] else {
                        panic!("fixture")
                    };
                    record[0] = Value::Text("00000000-0000-0000-0000-000000000000".into());
                }
                _ => records.clear(),
            }
            assert!(verify_document(&anchor(), &sign(&value, &root(), AAD), NOW).is_err());
        }
    }
    #[test]
    fn community_bad_signature_aad_all_truncated_prefixes_and_noncanonical_profile_fail_closed() {
        let wire = sign(&payload(), &root(), AAD);
        for prefix in 0..wire.len() {
            assert!(verify_document(&anchor(), &wire[..prefix], NOW).is_err());
        }
        assert!(verify_document(&anchor(), &sign(&payload(), &device_key(), AAD), NOW).is_err());
        assert!(verify_document(&anchor(), &sign(&payload(), &root(), GROUP_AAD), NOW).is_err());
        let mut bad = wire.clone();
        let n = bad.len();
        bad[n - 1] ^= 1;
        assert!(verify_document(&anchor(), &bad, NOW).is_err());
        let noncanonical = vec![0x98, 0x00];
        assert!(decode(&noncanonical).is_err());
        assert!(decode(&vec![0x81; 4096]).is_err());
        assert!(decode(&vec![0; MAX_WIRE + 1]).is_err());
    }
    #[test]
    fn voice_creation_wrong_certificate_ref_generation_device_scope_expiry_and_signed_domain_denied()
     {
        let verified = verify_document(&anchor(), &sign(&payload(), &root(), AAD), NOW).unwrap();
        for fault in 0..9 {
            let mut value = proposal(&verified);
            let Value::Array(fields) = &mut value else {
                panic!("fixture")
            };
            match fault {
                0 => fields[0] = Value::Text(DOMAIN.into()),
                1 => fields[2] = Value::Text("https://other.example".into()),
                2 => fields[3] = Value::Text("other-community".into()),
                3 => fields[8] = Value::Text("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee".into()),
                4 => fields[9] = Value::Integer(2.into()),
                5 => fields[10] = Value::Bytes(vec![99; 32]),
                6 => fields[11] = Value::Integer((NOW + 1).into()),
                7 => fields[12] = Value::Integer((NOW + MAX_CREATION_LIFETIME + 1).into()),
                _ => fields[6] = Value::Bytes(vec![0; 32]),
            }
            assert!(
                verify_creation(&verified, &sign(&value, &device_key(), GROUP_AAD), NOW).is_err()
            );
        }
        let valid = sign(&proposal(&verified), &device_key(), GROUP_AAD);
        assert!(verify_creation(&verified, &valid, NOW + 100).is_err());
        assert!(
            verify_creation(
                &verified,
                &sign(&proposal(&verified), &device_key(), AAD),
                NOW
            )
            .is_err()
        );
    }
}
