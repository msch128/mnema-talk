use super::*;
use coset::{
    CoseSign1, CoseSign1Builder, Header, HeaderBuilder, TaggedCborSerializable, cbor::value::Value,
    iana,
};
use ed25519_dalek::{Signature, Signer, SigningKey, VerifyingKey};
use mnema_crypto_adapter_candidate::Scope;
use sha2::{Digest, Sha256};
const INV_DOMAIN: &str = "MnemaTalk PairingInvitation";
const PROOF_DOMAIN: &str = "MnemaTalk PairingDeviceProof";
const INV_AAD: &[u8] = b"MnemaTalk PairingInvitation/v1";
const PROOF_AAD: &[u8] = b"MnemaTalk PairingDeviceProof/v1";
const PROTECTED: &[u8] = &[0xa1, 0x01, 0x32];
const MAX_WIRE: usize = 8192;
const MAX_NODES: usize = 256;
#[derive(Clone)]
pub struct NativeRootPin {
    pub(crate) scope: Scope,
    pub(crate) group: Vec<u8>,
    pub(crate) authority: [u8; 32],
}
impl NativeRootPin {
    /// Native out-of-band input; validates shape, never proves a human approved it.
    pub fn from_out_of_band(scope: Scope, group: &[u8], authority: [u8; 32]) -> Result<Self> {
        if group.is_empty() || group.len() > 128 {
            return Err(Error::Invalid);
        }
        key(&authority)?;
        Ok(Self {
            scope,
            group: group.into(),
            authority,
        })
    }
    pub fn authority_key(&self) -> [u8; 32] {
        self.authority
    }
    pub fn fingerprint(&self) -> Result<[u8; 32]> {
        Ok(Sha256::digest(encode(&Value::Array(vec![
            Value::Text("MnemaTalk NativeRootFingerprint/v1".into()),
            Value::Text(self.scope.origin().into()),
            Value::Text(self.scope.community().into()),
            Value::Bytes(self.group.clone()),
            Value::Bytes(self.authority.into()),
        ]))?)
        .into())
    }
}
pub struct NativeAdmissionIntent {
    pub(crate) account: String,
    pub(crate) device: String,
    pub(crate) identity: Vec<u8>,
    pub(crate) key: [u8; 32],
}
impl NativeAdmissionIntent {
    /// Exact device-key pin from an independently reviewed native/admin OOB workflow.
    /// No renderer/network boolean or unreviewed request-to-sign is accepted here.
    pub fn from_native_out_of_band_pin(
        account: &str,
        device: &str,
        identity: &[u8],
        device_key: [u8; 32],
    ) -> Result<Self> {
        name(account)?;
        name(device)?;
        if identity.is_empty() || identity.len() > 256 {
            return Err(Error::Invalid);
        }
        key(&device_key)?;
        Ok(Self {
            account: account.into(),
            device: device.into(),
            identity: identity.into(),
            key: device_key,
        })
    }
}
pub struct Device<'a, S: NativeSecrets> {
    secrets: &'a S,
    pin: NativeRootPin,
    key: [u8; 32],
}
impl<'a, S: NativeSecrets> Device<'a, S> {
    pub fn provision_native(secrets: &'a S, pin: NativeRootPin) -> Result<Self> {
        let key = secrets.create_seed("device-key")?;
        Ok(Self { secrets, pin, key })
    }
    pub fn reopen_native(secrets: &'a S, pin: NativeRootPin) -> Result<Self> {
        let seed = secrets.read_seed("device-key")?;
        let key = SigningKey::from_bytes(&seed).verifying_key().to_bytes();
        Ok(Self { secrets, pin, key })
    }
    pub fn public_key(&self) -> [u8; 32] {
        self.key
    }
    pub fn respond(&self, invitation: &[u8], now: u64) -> Result<ProofResponse> {
        let approved = verify_invitation(&self.pin, invitation, now)?;
        if approved.key != self.key {
            return Err(Error::Trust);
        }
        let seed = self.secrets.read_seed("device-key")?;
        let signer = SigningKey::from_bytes(&seed);
        if signer.verifying_key().to_bytes() != self.key {
            return Err(Error::Trust);
        }
        let proof = sign(&proof_value(&approved, invitation), &signer, PROOF_AAD)?;
        Ok(ProofResponse {
            invitation: invitation.into(),
            proof,
        })
    }
}
#[derive(Clone)]
pub struct ProofResponse {
    pub invitation: Vec<u8>,
    pub proof: Vec<u8>,
}
pub(crate) struct Invitation {
    pub account: String,
    pub device: String,
    pub identity: Vec<u8>,
    pub key: [u8; 32],
    pub nonce: [u8; 32],
    pub pin: NativeRootPin,
}
pub(crate) fn make_invitation(
    pin: &NativeRootPin,
    intent: &NativeAdmissionIntent,
    nonce: [u8; 32],
    now: u64,
    signer: &SigningKey,
) -> Result<Vec<u8>> {
    if signer.verifying_key().to_bytes() != pin.authority {
        return Err(Error::Trust);
    }
    let expires = now.checked_add(300).ok_or(Error::Limit)?;
    let value = Value::Array(vec![
        Value::Text(INV_DOMAIN.into()),
        Value::Integer(1.into()),
        Value::Text(pin.scope.origin().into()),
        Value::Text(pin.scope.community().into()),
        Value::Bytes(pin.group.clone()),
        Value::Text(intent.account.clone()),
        Value::Text(intent.device.clone()),
        Value::Bytes(intent.identity.clone()),
        Value::Bytes(intent.key.into()),
        Value::Bytes(nonce.into()),
        Value::Integer(now.into()),
        Value::Integer(expires.into()),
    ]);
    sign(&value, signer, INV_AAD)
}
pub(crate) fn verify_invitation(pin: &NativeRootPin, wire: &[u8], now: u64) -> Result<Invitation> {
    let value = verify(wire, &pin.authority, INV_AAD)?;
    let f = array(&value, 12)?;
    scope_fields(f, INV_DOMAIN, pin)?;
    let account = text(&f[5])?.to_owned();
    let device = text(&f[6])?.to_owned();
    name(&account)?;
    name(&device)?;
    let identity = bytes(&f[7])?.to_vec();
    if identity.is_empty() || identity.len() > 256 {
        return Err(Error::Invalid);
    }
    let device_key: [u8; 32] = bytes(&f[8])?.try_into().map_err(|_| Error::Invalid)?;
    key(&device_key)?;
    let nonce = bytes(&f[9])?.try_into().map_err(|_| Error::Invalid)?;
    let issued = integer(&f[10])?;
    let expires = integer(&f[11])?;
    if issued > now || now >= expires || expires.checked_sub(issued).ok_or(Error::Expired)? > 300 {
        return Err(Error::Expired);
    }
    Ok(Invitation {
        account,
        device,
        identity,
        key: device_key,
        nonce,
        pin: pin.clone(),
    })
}
fn proof_value(inv: &Invitation, wire: &[u8]) -> Value {
    Value::Array(vec![
        Value::Text(PROOF_DOMAIN.into()),
        Value::Integer(1.into()),
        Value::Text(inv.pin.scope.origin().into()),
        Value::Text(inv.pin.scope.community().into()),
        Value::Bytes(inv.pin.group.clone()),
        Value::Text(inv.account.clone()),
        Value::Text(inv.device.clone()),
        Value::Bytes(inv.identity.clone()),
        Value::Bytes(inv.key.into()),
        Value::Bytes(inv.nonce.into()),
        Value::Bytes(Sha256::digest(wire).to_vec()),
    ])
}
pub(crate) fn verify_response(
    pin: &NativeRootPin,
    response: &ProofResponse,
    now: u64,
) -> Result<Invitation> {
    let inv = verify_invitation(pin, &response.invitation, now)?;
    let value = verify(&response.proof, &inv.key, PROOF_AAD)?;
    if value != proof_value(&inv, &response.invitation) {
        return Err(Error::Trust);
    }
    Ok(inv)
}
pub(crate) fn sign(value: &Value, signer: &SigningKey, aad: &[u8]) -> Result<Vec<u8>> {
    let wire = CoseSign1Builder::new()
        .protected(
            HeaderBuilder::new()
                .algorithm(iana::Algorithm::Ed25519)
                .build(),
        )
        .payload(encode(value)?)
        .create_signature(aad, |transcript| {
            signer.sign(transcript).to_bytes().to_vec()
        })
        .build()
        .to_tagged_vec()
        .map_err(|_| Error::Invalid)?;
    if wire.len() > MAX_WIRE {
        return Err(Error::Limit);
    }
    Ok(wire)
}
fn verify(wire: &[u8], public: &[u8; 32], aad: &[u8]) -> Result<Value> {
    let outer = decode(wire)?;
    let Value::Tag(18, body) = outer else {
        return Err(Error::Invalid);
    };
    let f = array(&body, 4)?;
    if bytes(&f[0])? != PROTECTED
        || !matches!(&f[1],Value::Map(m)if m.is_empty())
        || bytes(&f[3])?.len() != 64
    {
        return Err(Error::Invalid);
    }
    let payload = decode(bytes(&f[2])?)?;
    let signed = CoseSign1::from_tagged_slice(wire).map_err(|_| Error::Invalid)?;
    if signed.unprotected != Header::default() {
        return Err(Error::Invalid);
    }
    let public = key(public)?;
    signed.verify_signature(aad, |s, t| {
        public
            .verify_strict(t, &Signature::from_slice(s).map_err(|_| Error::Signature)?)
            .map_err(|_| Error::Signature)
    })?;
    Ok(payload)
}
fn scope_fields(f: &[Value], domain: &str, pin: &NativeRootPin) -> Result<()> {
    if text(&f[0])? != domain
        || integer(&f[1])? != 1
        || text(&f[2])? != pin.scope.origin()
        || text(&f[3])? != pin.scope.community()
        || bytes(&f[4])? != pin.group
    {
        return Err(Error::Trust);
    }
    Ok(())
}
fn name(v: &str) -> Result<()> {
    if v.is_empty()
        || v.len() > 128
        || !v
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"-_".contains(&b))
    {
        Err(Error::Invalid)
    } else {
        Ok(())
    }
}
fn key(bytes: &[u8; 32]) -> Result<VerifyingKey> {
    let key = VerifyingKey::from_bytes(bytes).map_err(|_| Error::Invalid)?;
    if key.is_weak() {
        Err(Error::Invalid)
    } else {
        Ok(key)
    }
}
fn bytes(v: &Value) -> Result<&[u8]> {
    if let Value::Bytes(b) = v {
        Ok(b)
    } else {
        Err(Error::Invalid)
    }
}
fn text(v: &Value) -> Result<&str> {
    if let Value::Text(t) = v {
        Ok(t)
    } else {
        Err(Error::Invalid)
    }
}
fn integer(v: &Value) -> Result<u64> {
    if let Value::Integer(n) = v {
        u64::try_from(*n).map_err(|_| Error::Invalid)
    } else {
        Err(Error::Invalid)
    }
}
fn array(v: &Value, n: usize) -> Result<&[Value]> {
    if let Value::Array(a) = v
        && a.len() == n
    {
        Ok(a)
    } else {
        Err(Error::Invalid)
    }
}
pub(crate) fn encode(v: &Value) -> Result<Vec<u8>> {
    let mut wire = Vec::new();
    coset::cbor::ser::into_writer(v, &mut wire).map_err(|_| Error::Invalid)?;
    Ok(wire)
}
fn decode(wire: &[u8]) -> Result<Value> {
    preflight(wire)?;
    let value: Value = coset::cbor::de::from_reader(wire).map_err(|_| Error::Invalid)?;
    if encode(&value)? != wire {
        return Err(Error::Invalid);
    }
    Ok(value)
}
// Structural scanner adapted verbatim in algorithm from independently reviewed trust
// candidate; lower pairing budgets. COSE/cryptography remain established libraries.
fn preflight(wire: &[u8]) -> Result<()> {
    if wire.is_empty() || wire.len() > MAX_WIRE {
        return Err(Error::Limit);
    }
    let (mut index, mut nodes) = (0usize, 0usize);
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
        let byte = *wire.get(index).ok_or(Error::Invalid)?;
        index += 1;
        let major = byte >> 5;
        let additional = byte & 31;
        let argument = match additional {
            0..=23 => u64::from(additional),
            24..=27 => {
                let width = 1usize << (additional - 24);
                let end = index.checked_add(width).ok_or(Error::Limit)?;
                let bytes = wire.get(index..end).ok_or(Error::Invalid)?;
                let mut value = 0u64;
                for byte in bytes {
                    value = (value << 8) | u64::from(*byte);
                }
                let min = match width {
                    1 => 24,
                    2 => 256,
                    4 => 65536,
                    _ => 1u64 << 32,
                };
                if value < min {
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
                let size = usize::try_from(argument).map_err(|_| Error::Limit)?;
                index = index.checked_add(size).ok_or(Error::Limit)?;
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
