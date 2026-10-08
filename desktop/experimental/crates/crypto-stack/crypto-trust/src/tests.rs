use super::*;
use coset::{CoseSign1Builder, HeaderBuilder, iana};
use ed25519_dalek::{Signer, SigningKey};
use openmls::prelude::{
    BasicCredential, Ciphersuite, CredentialWithKey, GroupId, KeyPackage, KeyPackageBundle,
    MlsGroup, MlsGroupCreateConfig, MlsMessageBodyIn, MlsMessageIn, SignatureScheme, StagedWelcome,
};
use openmls_basic_credential::SignatureKeyPair;

const NOW: u64 = 1000;
fn signer(byte: u8) -> SigningKey {
    SigningKey::from_bytes(&[byte; 32])
}
fn scope() -> Scope {
    Scope::new("https://community.example", "community").unwrap()
}
fn verifier() -> TrustVerifier {
    TrustVerifier::from_native_pin(
        scope(),
        signer(1).verifying_key().as_bytes(),
        vec![
            NativeGroupFloor::from_native_store(b"group-a", 0).unwrap(),
            NativeGroupFloor::from_native_store(b"group-b", 0).unwrap(),
        ],
        NOW,
    )
    .unwrap()
}
fn record(account: &str, device: &str, identity: &[u8], key: &[u8]) -> Value {
    Value::Array(vec![
        Value::Text(account.into()),
        Value::Text(device.into()),
        Value::Bytes(identity.into()),
        Value::Bytes(key.into()),
    ])
}
fn claims(generation: u64, records: Vec<Value>) -> Value {
    Value::Array(vec![
        Value::Text(DOMAIN.into()),
        Value::Integer(1.into()),
        Value::Text(scope().origin().into()),
        Value::Text(scope().community().into()),
        Value::Bytes(b"group-a".to_vec()),
        Value::Integer(generation.into()),
        Value::Integer(NOW.into()),
        Value::Integer((NOW + 1000).into()),
        Value::Array(records),
    ])
}
fn default_claims(generation: u64) -> Value {
    claims(
        generation,
        vec![record(
            "account",
            "device",
            b"identity",
            signer(2).verifying_key().as_bytes(),
        )],
    )
}
fn change(value: &mut Value, index: usize, replacement: Value) {
    let Value::Array(fields) = value else {
        panic!("test fixture array");
    };
    fields[index] = replacement;
}
fn signed_payload(payload: Vec<u8>, authority: &SigningKey, aad: &[u8], header: Header) -> Vec<u8> {
    CoseSign1Builder::new()
        .protected(header)
        .payload(payload)
        .create_signature(aad, |transcript| {
            authority.sign(transcript).to_bytes().to_vec()
        })
        .build()
        .to_tagged_vec()
        .unwrap()
}
fn signed(value: &Value) -> Vec<u8> {
    signed_payload(
        encode(value).unwrap(),
        &signer(1),
        AAD,
        HeaderBuilder::new()
            .algorithm(iana::Algorithm::Ed25519)
            .build(),
    )
}
fn verify(value: &Value) -> Result<VerifiedRoster> {
    verifier().verify_roster(&signed(value), NOW)
}

#[test]
fn exact_standard_cose_transcript_and_verified_device_binding() {
    let mut verifier = verifier();
    let payload = encode(&default_claims(1)).unwrap();
    let wire = signed(&default_claims(1));
    let sign1 = CoseSign1::from_tagged_slice(&wire).unwrap();
    let independent_structure = Value::Array(vec![
        Value::Text("Signature1".into()),
        Value::Bytes(PROTECTED.to_vec()),
        Value::Bytes(AAD.to_vec()),
        Value::Bytes(payload),
    ]);
    assert_eq!(sign1.tbs_data(AAD), encode(&independent_structure).unwrap());
    let handle = verifier.verify_roster(&wire, NOW).unwrap();
    let key = signer(2).verifying_key().to_bytes();
    let proof = verifier
        .verify_device(&handle, b"group-a", b"identity", &key, NOW)
        .unwrap();
    assert_eq!(proof.account(), "account");
    assert_eq!(proof.device(), "device");
    verifier
        .validate_device(&proof, b"group-a", b"identity", &key, NOW)
        .unwrap();
    assert!(matches!(
        verifier.verify_device(&handle, b"group-a", b"other", &key, NOW),
        Err(Error::UnapprovedDevice)
    ));
    assert!(matches!(
        verifier.verify_device(
            &handle,
            b"group-a",
            b"identity",
            signer(3).verifying_key().as_bytes(),
            NOW
        ),
        Err(Error::UnapprovedDevice)
    ));
}

#[test]
fn official_rfc8032_empty_message_known_answer_uses_strict_library_verification() {
    // RFC8032 §7.1 TEST1 public fixture, not an application secret.
    fn hex(value: &str) -> Vec<u8> {
        let (pairs, remainder) = value.as_bytes().as_chunks::<2>();
        assert!(remainder.is_empty());
        pairs
            .iter()
            .map(|b| u8::from_str_radix(std::str::from_utf8(b).unwrap(), 16).unwrap())
            .collect()
    }
    let key: [u8; 32] = hex("d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a")
        .try_into()
        .unwrap();
    let signature = Signature::from_slice(&hex(concat!(
        "e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e065224901555f",
        "b8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b"
    )))
    .unwrap();
    let key = VerifyingKey::from_bytes(&key).unwrap();
    key.verify_strict(b"", &signature).unwrap();
    assert!(key.verify_strict(b"changed", &signature).is_err());
}

#[test]
fn unknown_signer_signature_tamper_and_each_unsigned_claim_tamper_reject() {
    let value = default_claims(1);
    let wrong = signed_payload(
        encode(&value).unwrap(),
        &signer(9),
        AAD,
        HeaderBuilder::new()
            .algorithm(iana::Algorithm::Ed25519)
            .build(),
    );
    assert_eq!(
        verifier().verify_roster(&wrong, NOW),
        Err(Error::BadSignature)
    );
    let wire = signed(&value);
    let original = CoseSign1::from_tagged_slice(&wire).unwrap();
    for index in 0..64 {
        let mut changed = original.clone();
        changed.signature[index] ^= 1;
        assert_eq!(
            verifier().verify_roster(&changed.to_tagged_vec().unwrap(), NOW),
            Err(Error::BadSignature)
        );
    }
    for index in 0..9 {
        let mut changed_claims = value.clone();
        change(&mut changed_claims, index, Value::Text("tampered".into()));
        let mut changed = original.clone();
        changed.payload = Some(encode(&changed_claims).unwrap());
        assert_eq!(
            verifier().verify_roster(&changed.to_tagged_vec().unwrap(), NOW),
            Err(Error::BadSignature)
        );
    }
    let wrong_aad = signed_payload(
        encode(&value).unwrap(),
        &signer(1),
        b"different protocol",
        HeaderBuilder::new()
            .algorithm(iana::Algorithm::Ed25519)
            .build(),
    );
    assert_eq!(
        verifier().verify_roster(&wrong_aad, NOW),
        Err(Error::BadSignature)
    );
}

#[test]
fn incompatible_cose_profiles_and_extra_unprotected_header_reject() {
    let payload = encode(&default_claims(1)).unwrap();
    let legacy = signed_payload(
        payload.clone(),
        &signer(1),
        AAD,
        HeaderBuilder::new()
            .algorithm(iana::Algorithm::EdDSA)
            .build(),
    );
    assert_eq!(
        verifier().verify_roster(&legacy, NOW),
        Err(Error::UnsupportedProfile)
    );
    let extra = signed_payload(
        payload,
        &signer(1),
        AAD,
        HeaderBuilder::new()
            .algorithm(iana::Algorithm::Ed25519)
            .key_id(vec![1])
            .build(),
    );
    assert_eq!(
        verifier().verify_roster(&extra, NOW),
        Err(Error::UnsupportedProfile)
    );
    let mut changed = CoseSign1::from_tagged_slice(&signed(&default_claims(1))).unwrap();
    changed.unprotected = HeaderBuilder::new().key_id(vec![1]).build();
    // COSE correctly leaves unprotected headers outside authentication; profile denies them.
    assert_eq!(
        verifier().verify_roster(&changed.clone().to_tagged_vec().unwrap(), NOW),
        Err(Error::UnsupportedProfile)
    );
    changed.payload = None;
    assert!(
        verifier()
            .verify_roster(&changed.to_tagged_vec().unwrap(), NOW)
            .is_err()
    );
    let untagged = coset::CborSerializable::to_vec(
        CoseSign1::from_tagged_slice(&signed(&default_claims(1))).unwrap(),
    )
    .unwrap();
    assert_eq!(
        verifier().verify_roster(&untagged, NOW),
        Err(Error::UnsupportedProfile)
    );
}

#[test]
fn authenticated_wrong_scope_group_version_domain_are_not_authorization() {
    for (index, replacement, error) in [
        (
            0,
            Value::Text("Different protocol".into()),
            Error::UnsupportedProfile,
        ),
        (1, Value::Integer(2.into()), Error::UnsupportedProfile),
        (
            2,
            Value::Text("https://other.example".into()),
            Error::WrongScope,
        ),
        (
            2,
            Value::Text("https://community.example:444".into()),
            Error::WrongScope,
        ),
        (
            2,
            Value::Text("https://community.example/".into()),
            Error::WrongScope,
        ),
        (3, Value::Text("other".into()), Error::WrongScope),
        (
            4,
            Value::Bytes(b"unknown-group".to_vec()),
            Error::UnknownGroup,
        ),
    ] {
        let mut value = default_claims(1);
        change(&mut value, index, replacement);
        assert_eq!(verify(&value), Err(error));
    }
}

#[test]
fn instance_group_and_exact_device_proof_reuse_fail_closed() {
    let mut one = verifier();
    let mut two = verifier();
    let handle = one.verify_roster(&signed(&default_claims(1)), NOW).unwrap();
    let key = signer(2).verifying_key().to_bytes();
    let proof = one
        .verify_device(&handle, b"group-a", b"identity", &key, NOW)
        .unwrap();
    assert!(matches!(
        two.verify_device(&handle, b"group-a", b"identity", &key, NOW),
        Err(Error::StaleHandle)
    ));
    assert_eq!(
        two.validate_device(&proof, b"group-a", b"identity", &key, NOW),
        Err(Error::StaleHandle)
    );
    assert_eq!(
        one.validate_device(&proof, b"group-b", b"identity", &key, NOW),
        Err(Error::StaleHandle)
    );
    assert_eq!(
        one.validate_device(
            &proof,
            b"group-a",
            b"identity",
            signer(3).verifying_key().as_bytes(),
            NOW
        ),
        Err(Error::UnapprovedDevice)
    );
}

#[test]
fn replay_revocation_and_full_snapshot_generation_replace_old_approvals() {
    let mut verifier = verifier();
    let wire = signed(&default_claims(1));
    let handle = verifier.verify_roster(&wire, NOW).unwrap();
    let key = signer(2).verifying_key().to_bytes();
    let proof = verifier
        .verify_device(&handle, b"group-a", b"identity", &key, NOW)
        .unwrap();
    assert_eq!(verifier.verify_roster(&wire, NOW), Err(Error::Replay));
    let replacement = claims(
        8,
        vec![record(
            "other",
            "device",
            b"other",
            signer(3).verifying_key().as_bytes(),
        )],
    );
    let next = verifier.verify_roster(&signed(&replacement), NOW).unwrap();
    assert_eq!(
        verifier.validate_device(&proof, b"group-a", b"identity", &key, NOW),
        Err(Error::StaleHandle)
    );
    assert!(matches!(
        verifier.verify_device(&next, b"group-a", b"identity", &key, NOW),
        Err(Error::UnapprovedDevice)
    ));
    assert_eq!(verifier.verify_roster(&wire, NOW), Err(Error::Replay));
    let empty = verifier
        .verify_roster(&signed(&claims(9, vec![])), NOW)
        .unwrap();
    assert!(matches!(
        verifier.verify_device(
            &empty,
            b"group-a",
            b"other",
            signer(3).verifying_key().as_bytes(),
            NOW
        ),
        Err(Error::UnapprovedDevice)
    ));
}

#[test]
fn invalid_new_signature_or_records_never_consumes_high_water() {
    let mut verifier = verifier();
    let handle = verifier
        .verify_roster(&signed(&default_claims(1)), NOW)
        .unwrap();
    let mut invalid = CoseSign1::from_tagged_slice(&signed(&default_claims(900))).unwrap();
    invalid.signature[0] ^= 1;
    assert_eq!(
        verifier.verify_roster(&invalid.to_tagged_vec().unwrap(), NOW),
        Err(Error::BadSignature)
    );
    let duplicated = claims(
        2,
        vec![
            record(
                "account",
                "device",
                b"identity",
                signer(2).verifying_key().as_bytes()
            );
            2
        ],
    );
    assert_eq!(
        verifier.verify_roster(&signed(&duplicated), NOW),
        Err(Error::ConflictingRecord)
    );
    assert!(
        verifier
            .verify_device(
                &handle,
                b"group-a",
                b"identity",
                signer(2).verifying_key().as_bytes(),
                NOW
            )
            .is_ok()
    );
    assert!(
        verifier
            .verify_roster(&signed(&default_claims(2)), NOW)
            .is_ok()
    );
}

#[test]
fn expiry_future_long_lifetime_and_clock_rollback_reject() {
    let mut verifier = verifier();
    let handle = verifier
        .verify_roster(&signed(&default_claims(1)), NOW)
        .unwrap();
    assert_eq!(
        verifier.current(&handle, b"group-a", NOW - 1).err(),
        Some(Error::ClockRollback)
    );
    assert!(verifier.current(&handle, b"group-a", NOW + 999).is_ok());
    assert_eq!(
        verifier.current(&handle, b"group-a", NOW + 1000).err(),
        Some(Error::Expired)
    );
    assert_eq!(
        verifier.current(&handle, b"group-a", NOW + 999).err(),
        Some(Error::ClockRollback)
    );
    assert_eq!(
        verifier.verify_roster(&signed(&default_claims(2)), NOW + 1000),
        Err(Error::Expired)
    );
    for (issued, expires) in [
        (NOW + 1, NOW + 10),
        (NOW, NOW),
        (NOW, NOW + MAX_LIFETIME + 1),
        (u64::MAX - 1, u64::MAX),
    ] {
        let mut value = default_claims(1);
        change(&mut value, 6, Value::Integer(issued.into()));
        change(&mut value, 7, Value::Integer(expires.into()));
        assert_eq!(verify(&value), Err(Error::TimeInvalid));
    }
}

#[test]
fn native_generation_floors_are_enforced_without_overflow() {
    let key = signer(1).verifying_key().to_bytes();
    let mut verifier = TrustVerifier::from_native_pin(
        scope(),
        &key,
        vec![NativeGroupFloor::from_native_store(b"group-a", 10).unwrap()],
        NOW,
    )
    .unwrap();
    assert_eq!(
        verifier.verify_roster(&signed(&default_claims(10)), NOW),
        Err(Error::Replay)
    );
    assert!(
        verifier
            .verify_roster(&signed(&default_claims(11)), NOW)
            .is_ok()
    );
    assert!(
        verifier
            .verify_roster(&signed(&default_claims(u64::MAX)), NOW)
            .is_ok()
    );
    assert_eq!(
        verifier.verify_roster(&signed(&default_claims(u64::MAX)), NOW),
        Err(Error::Replay)
    );
}

#[test]
fn native_pin_rejects_weak_key_duplicate_groups_and_bad_shapes() {
    let floor = || NativeGroupFloor::from_native_store(b"group-a", 0).unwrap();
    assert!(matches!(
        TrustVerifier::from_native_pin(scope(), &[0; 32], vec![floor()], NOW),
        Err(Error::InvalidPin)
    ));
    assert!(matches!(
        TrustVerifier::from_native_pin(scope(), &[0; 31], vec![floor()], NOW),
        Err(Error::InvalidPin)
    ));
    let key = signer(1).verifying_key().to_bytes();
    assert!(matches!(
        TrustVerifier::from_native_pin(scope(), &key, vec![floor(), floor()], NOW),
        Err(Error::InvalidPin)
    ));
    assert!(matches!(
        TrustVerifier::from_native_pin(scope(), &key, vec![], NOW),
        Err(Error::InvalidPin)
    ));
    assert!(NativeGroupFloor::from_native_store(b"", 0).is_err());
    assert!(NativeGroupFloor::from_native_store(&[1; 129], 0).is_err());
    let excessive_groups = (0..33)
        .map(|n| NativeGroupFloor::from_native_store(&[n], 0).unwrap())
        .collect();
    assert!(matches!(
        TrustVerifier::from_native_pin(scope(), &key, excessive_groups, NOW),
        Err(Error::InvalidPin)
    ));
}

#[test]
fn records_require_sorted_unique_identity_key_account_device_and_valid_keys() {
    let a = record("a", "device", b"a", signer(2).verifying_key().as_bytes());
    let b = record("b", "device", b"b", signer(3).verifying_key().as_bytes());
    assert!(verify(&claims(1, vec![a.clone(), b.clone()])).is_ok());
    for entries in [
        vec![b.clone(), a.clone()],
        vec![a.clone(), a.clone()],
        vec![
            a.clone(),
            record("b", "device", b"a", signer(3).verifying_key().as_bytes()),
        ],
        vec![
            a.clone(),
            record("b", "device", b"b", signer(2).verifying_key().as_bytes()),
        ],
    ] {
        assert_eq!(verify(&claims(1, entries)), Err(Error::ConflictingRecord));
    }
    for entry in [
        record("", "d", b"a", signer(2).verifying_key().as_bytes()),
        record("a", "not valid", b"a", signer(2).verifying_key().as_bytes()),
        record("a", "d", b"", signer(2).verifying_key().as_bytes()),
        record("a", "d", &[1; 257], signer(2).verifying_key().as_bytes()),
        record("a", "d", b"a", &[0; 32]),
        record("a", "d", b"a", &[2; 31]),
    ] {
        assert_eq!(verify(&claims(1, vec![entry])), Err(Error::InvalidRecord));
    }
    assert_eq!(verify(&claims(1, vec![a; 129])), Err(Error::Limit));
}

#[test]
fn malformed_truncated_nested_noncanonical_and_oversized_inputs_never_panic() {
    let wire = signed(&default_claims(1));
    for length in 0..wire.len() {
        assert!(verifier().verify_roster(&wire[..length], NOW).is_err());
    }
    let mut trailing = wire.clone();
    trailing.push(0);
    assert!(verifier().verify_roster(&trailing, NOW).is_err());
    assert_eq!(
        verifier().verify_roster(&vec![0; MAX_WIRE + 1], NOW),
        Err(Error::Limit)
    );
    let mut deep = vec![0x81; 10000];
    deep.push(0);
    assert_eq!(verifier().verify_roster(&deep, NOW), Err(Error::Limit));
    for length in 1..300 {
        let input: Vec<_> = (0..length)
            .map(|i| ((i * 31 + length * 17) % 256) as u8)
            .collect();
        assert!(verifier().verify_roster(&input, NOW).is_err());
    }
    // Tag18 in non-minimal integer encoding, even though original signature remains valid.
    let mut noncanonical = vec![0xd8, 18];
    noncanonical.extend_from_slice(&wire[1..]);
    assert!(verifier().verify_roster(&noncanonical, NOW).is_err());
    let mut payload = encode(&default_claims(1)).unwrap();
    // Definite top-level array9 changed to indefinite array, followed by break.
    payload[0] = 0x9f;
    payload.push(0xff);
    let signed = signed_payload(
        payload,
        &signer(1),
        AAD,
        HeaderBuilder::new()
            .algorithm(iana::Algorithm::Ed25519)
            .build(),
    );
    assert!(verifier().verify_roster(&signed, NOW).is_err());
    // Signed payload with hidden recursive structure gets screened independently.
    let mut payload = vec![0x81; 10000];
    payload.push(0);
    let signed = signed_payload(
        payload,
        &signer(1),
        AAD,
        HeaderBuilder::new()
            .algorithm(iana::Algorithm::Ed25519)
            .build(),
    );
    assert_eq!(verifier().verify_roster(&signed, NOW), Err(Error::Limit));
}

fn native_group(group_id: &[u8]) -> (OpenMlsRustCrypto, MlsGroup, Value) {
    let provider = OpenMlsRustCrypto::default();
    let signer = SignatureKeyPair::new(SignatureScheme::ED25519).unwrap();
    let credential = CredentialWithKey {
        credential: BasicCredential::new(b"alice".to_vec()).into(),
        signature_key: signer.to_public_vec().into(),
    };
    let config = MlsGroupCreateConfig::builder()
        .ciphersuite(Ciphersuite::MLS_128_DHKEMX25519_CHACHA20POLY1305_SHA256_Ed25519)
        .build();
    let group = MlsGroup::new_with_group_id(
        &provider,
        &signer,
        &config,
        GroupId::from_slice(group_id),
        credential,
    )
    .unwrap();
    (
        provider,
        group,
        record("alice", "desktop", b"alice", &signer.to_public_vec()),
    )
}

#[test]
fn actual_mls_group_binding_and_adapter_expiry_instance_revocation_gates() {
    let mut verifier = verifier();
    let (provider, group, entry) = native_group(b"group-a");
    let handle = verifier
        .verify_roster(&signed(&claims(1, vec![entry])), NOW)
        .unwrap();
    let bound = verifier
        .adopt_adapter(&handle, provider, group, NOW)
        .unwrap();
    assert_eq!(bound.summary(&mut verifier, NOW).unwrap().members, 1);
    let mut wrong = super::tests::verifier();
    assert_eq!(bound.summary(&mut wrong, NOW), Err(Error::StaleHandle));
    assert_eq!(
        bound.summary(&mut verifier, NOW + 1000),
        Err(Error::Expired)
    );

    let mut verifier = super::tests::verifier();
    let (provider, group, entry) = native_group(b"group-a");
    let handle = verifier
        .verify_roster(&signed(&claims(1, vec![entry])), NOW)
        .unwrap();
    let mut bound = verifier
        .adopt_adapter(&handle, provider, group, NOW)
        .unwrap();
    let revoked = verifier
        .verify_roster(&signed(&claims(2, vec![])), NOW)
        .unwrap();
    assert_eq!(bound.summary(&mut verifier, NOW), Err(Error::StaleHandle));
    assert!(matches!(
        bound.stage_commit(&mut verifier, b"badwire", NOW),
        Err(Error::StaleHandle)
    ));
    assert!(matches!(
        bound.receive_application(&mut verifier, b"badwire", NOW),
        Err(Error::StaleHandle)
    ));
    let (provider, group, _) = native_group(b"group-a");
    assert!(matches!(
        verifier.adopt_adapter(&revoked, provider, group, NOW),
        Err(Error::UnapprovedDevice)
    ));

    let mut verifier = super::tests::verifier();
    let (_, _, entry) = native_group(b"group-a");
    let handle = verifier
        .verify_roster(&signed(&claims(1, vec![entry])), NOW)
        .unwrap();
    let (provider, wrong_group, _) = native_group(b"group-b");
    assert!(matches!(
        verifier.adopt_adapter(&handle, provider, wrong_group, NOW),
        Err(Error::StaleHandle)
    ));
}

struct LiveFixture {
    verifier: TrustVerifier,
    bound: GroupBoundAdapter,
    bob_provider: OpenMlsRustCrypto,
    bob_group: MlsGroup,
    bob_signer: SignatureKeyPair,
    next_package: KeyPackageBundle,
    records: Vec<Value>,
}
impl LiveFixture {
    fn new(approve_next: bool, spoof: bool) -> Self {
        let suite = Ciphersuite::MLS_128_DHKEMX25519_CHACHA20POLY1305_SHA256_Ed25519;
        let alice_provider = OpenMlsRustCrypto::default();
        let bob_provider = OpenMlsRustCrypto::default();
        let next_provider = OpenMlsRustCrypto::default();
        let identity = |name: &[u8]| {
            let signer = SignatureKeyPair::new(SignatureScheme::ED25519).unwrap();
            let credential = CredentialWithKey {
                credential: BasicCredential::new(name.to_vec()).into(),
                signature_key: signer.to_public_vec().into(),
            };
            (credential, signer)
        };
        let (alice_credential, alice_signer) = identity(b"alice");
        let (bob_credential, bob_signer) = identity(b"bob");
        let (next_credential, next_signer) = identity(if spoof { b"alice" } else { b"charlie" });
        let mut records = vec![
            record("alice", "desktop", b"alice", &alice_signer.to_public_vec()),
            record("bob", "browser", b"bob", &bob_signer.to_public_vec()),
        ];
        if approve_next {
            records.push(record(
                "charlie",
                "desktop",
                b"charlie",
                &next_signer.to_public_vec(),
            ));
        }
        let config = MlsGroupCreateConfig::builder()
            .ciphersuite(suite)
            .use_ratchet_tree_extension(true)
            .build();
        let mut alice_group = MlsGroup::new_with_group_id(
            &alice_provider,
            &alice_signer,
            &config,
            GroupId::from_slice(b"group-a"),
            alice_credential,
        )
        .unwrap();
        let bob_package = KeyPackage::builder()
            .build(suite, &bob_provider, &bob_signer, bob_credential)
            .unwrap();
        let (_, welcome, _) = alice_group
            .add_members(
                &alice_provider,
                &alice_signer,
                std::slice::from_ref(bob_package.key_package()),
            )
            .unwrap();
        alice_group.merge_pending_commit(&alice_provider).unwrap();
        let welcome: MlsMessageIn = welcome.into();
        let MlsMessageBodyIn::Welcome(welcome) = welcome.extract() else {
            panic!("test welcome");
        };
        let bob_group =
            StagedWelcome::new_from_welcome(&bob_provider, config.join_config(), welcome, None)
                .unwrap()
                .into_group(&bob_provider)
                .unwrap();
        let next_package = KeyPackage::builder()
            .build(suite, &next_provider, &next_signer, next_credential)
            .unwrap();
        let mut verifier = verifier();
        let handle = verifier
            .verify_roster(&signed(&claims(1, records.clone())), NOW)
            .unwrap();
        let bound = verifier
            .adopt_adapter(&handle, alice_provider, alice_group, NOW)
            .unwrap();
        Self {
            verifier,
            bound,
            bob_provider,
            bob_group,
            bob_signer,
            next_package,
            records,
        }
    }
    fn commit(&mut self) -> Vec<u8> {
        let (commit, _, _) = self
            .bob_group
            .add_members(
                &self.bob_provider,
                &self.bob_signer,
                std::slice::from_ref(self.next_package.key_package()),
            )
            .unwrap();
        commit.to_bytes().unwrap()
    }
}

#[test]
fn signed_roster_gates_actual_mls_application_and_approved_explicit_add_merge() {
    let mut fixture = LiveFixture::new(true, false);
    let app = fixture
        .bob_group
        .create_message(
            &fixture.bob_provider,
            &fixture.bob_signer,
            b"protected text",
        )
        .unwrap()
        .to_bytes()
        .unwrap();
    assert_eq!(
        fixture
            .bound
            .receive_application(&mut fixture.verifier, &app, NOW)
            .unwrap(),
        b"protected text"
    );
    assert!(
        fixture
            .bound
            .receive_application(&mut fixture.verifier, &app, NOW)
            .is_err()
    );
    let commit = fixture.commit();
    let stage = fixture
        .bound
        .stage_commit(&mut fixture.verifier, &commit, NOW)
        .unwrap();
    assert!(matches!(
        fixture
            .bound
            .authorize_stage(&mut fixture.verifier, stage, NOW),
        Err(Error::Adapter(
            mnema_crypto_adapter_candidate::Error::NotInspected
        ))
    ));
    let inspected = fixture
        .bound
        .inspect_stage(&mut fixture.verifier, stage, NOW)
        .unwrap();
    assert_eq!(inspected.additions.len(), 1);
    let approval = fixture
        .bound
        .authorize_stage(&mut fixture.verifier, stage, NOW)
        .unwrap();
    assert_eq!(
        fixture
            .bound
            .merge_authorized(&mut fixture.verifier, approval, NOW)
            .unwrap()
            .members,
        3
    );
}

#[test]
fn valid_member_cannot_enroll_unknown_or_spoofed_identity_without_signed_roster_approval() {
    for spoof in [false, true] {
        let mut fixture = LiveFixture::new(false, spoof);
        let before = fixture.bound.summary(&mut fixture.verifier, NOW).unwrap();
        let commit = fixture.commit();
        let stage = fixture
            .bound
            .stage_commit(&mut fixture.verifier, &commit, NOW)
            .unwrap();
        fixture
            .bound
            .inspect_stage(&mut fixture.verifier, stage, NOW)
            .unwrap();
        assert!(matches!(
            fixture
                .bound
                .authorize_stage(&mut fixture.verifier, stage, NOW),
            Err(Error::Adapter(
                mnema_crypto_adapter_candidate::Error::UnapprovedDevice
            ))
        ));
        assert_eq!(
            fixture.bound.summary(&mut fixture.verifier, NOW).unwrap(),
            before
        );
        fixture
            .bound
            .reject_stage(&mut fixture.verifier, stage, NOW)
            .unwrap();
    }
}

#[test]
fn expiry_or_new_signed_roster_between_authorization_and_merge_preserves_epoch() {
    for expire in [false, true] {
        let mut fixture = LiveFixture::new(true, false);
        let before = fixture.bound.summary(&mut fixture.verifier, NOW).unwrap();
        let commit = fixture.commit();
        let stage = fixture
            .bound
            .stage_commit(&mut fixture.verifier, &commit, NOW)
            .unwrap();
        fixture
            .bound
            .inspect_stage(&mut fixture.verifier, stage, NOW)
            .unwrap();
        let approval = fixture
            .bound
            .authorize_stage(&mut fixture.verifier, stage, NOW)
            .unwrap();
        if expire {
            assert_eq!(
                fixture
                    .bound
                    .merge_authorized(&mut fixture.verifier, approval, NOW + 1000),
                Err(Error::Expired)
            );
        } else {
            fixture.records.pop(); // revoke pending new device in real signed generation2 roster
            fixture
                .verifier
                .verify_roster(&signed(&claims(2, fixture.records)), NOW)
                .unwrap();
            assert_eq!(
                fixture
                    .bound
                    .merge_authorized(&mut fixture.verifier, approval, NOW),
                Err(Error::StaleHandle)
            );
        }
        // Read-only state assertion: denied wrapper operation never reached merge.
        assert_eq!(
            fixture.bound.adapter.summary(fixture.bound.group).unwrap(),
            before
        );
    }
}
