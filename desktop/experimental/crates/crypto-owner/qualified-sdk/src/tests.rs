use super::*;
use coset::{CoseSign1Builder, HeaderBuilder, iana};
use ed25519_dalek::{Signer, SigningKey};
use openmls::prelude::tls_codec::Serialize as _;
use openmls_rust_crypto::OpenMlsRustCrypto;
use sha2::Digest;
use std::path::PathBuf;
const GROUP: &[u8] = b"qualification-group";
const NOW: u64 = 1000;
const TEXT: &[u8] = b"fixture-only public qualification text";
struct FixtureKeys;
impl NativeKeyProvider for FixtureKeys {
    fn database_key(&self) -> Result<Zeroizing<[u8; 32]>> {
        Ok(Zeroizing::new([77; 32]))
    }
}
struct Locked;
impl NativeKeyProvider for Locked {
    fn database_key(&self) -> Result<Zeroizing<[u8; 32]>> {
        Err(Error::KeyUnavailable)
    }
}
struct Wrong;
impl NativeKeyProvider for Wrong {
    fn database_key(&self) -> Result<Zeroizing<[u8; 32]>> {
        Ok(Zeroizing::new([78; 32]))
    }
}
fn authority() -> SigningKey {
    SigningKey::from_bytes(&[1; 32])
} // Public deterministic fixture only.
fn scope() -> Scope {
    Scope::new("https://community.example", "community").unwrap()
}
fn pin(peer: &[u8]) -> NativeBootstrap {
    NativeBootstrap::from_native_pin(
        scope(),
        GROUP,
        authority().verifying_key().to_bytes(),
        b"alice",
        peer.try_into().unwrap(),
    )
    .unwrap()
}
fn encode(value: &Value) -> Vec<u8> {
    let mut wire = Vec::new();
    coset::cbor::ser::into_writer(value, &mut wire).unwrap();
    wire
}
fn roster(generation: u64, entries: Vec<(&str, &str, &[u8], &[u8])>) -> Vec<u8> {
    let records = entries
        .into_iter()
        .map(|(a, d, i, k)| {
            Value::Array(vec![
                Value::Text(a.into()),
                Value::Text(d.into()),
                Value::Bytes(i.into()),
                Value::Bytes(k.into()),
            ])
        })
        .collect();
    let value = Value::Array(vec![
        Value::Text("MnemaTalk DeviceAuthorization".into()),
        Value::Integer(1.into()),
        Value::Text(scope().origin().into()),
        Value::Text(scope().community().into()),
        Value::Bytes(GROUP.into()),
        Value::Integer(generation.into()),
        Value::Integer(NOW.into()),
        Value::Integer((NOW + 1000).into()),
        Value::Array(records),
    ]);
    CoseSign1Builder::new()
        .protected(
            HeaderBuilder::new()
                .algorithm(iana::Algorithm::Ed25519)
                .build(),
        )
        .payload(encode(&value))
        .create_signature(b"MnemaTalk DeviceAuthorization/v1", |t| {
            authority().sign(t).to_bytes().to_vec()
        })
        .build()
        .to_tagged_vec()
        .unwrap()
}
fn run_dir() -> tempfile::TempDir {
    let root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("evidence/runs");
    std::fs::create_dir_all(&root).unwrap();
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&root, std::fs::Permissions::from_mode(0o700)).unwrap();
    }
    tempfile::Builder::new()
        .prefix("private-fixture-")
        .tempdir_in(root)
        .unwrap()
}
struct Fixture {
    core: Core,
    alice_provider: OpenMlsRustCrypto,
    alice_group: MlsGroup,
    alice_signer: SignatureKeyPair,
    offer: EnrollmentOffer,
    welcome: Vec<u8>,
}

const CHANNEL: &str = "11111111-1111-4111-8111-111111111111";
const EVENT: &str = "22222222-2222-4222-8222-222222222222";
const SOURCE: &str = "33333333-3333-4333-8333-333333333333";
fn binding() -> NativeBinding {
    NativeBinding::from_native_actor(CHANNEL, "profile", "main", "session", "bob", "desktop")
        .unwrap()
}
fn sdk_fixture(path: &Path) -> (Sdk, Fixture) {
    let mut fixture = Fixture::offered(path);
    fixture.live();
    // Preserve the actual opposing peer while moving the sole native Core owner.
    let placeholder =
        Core::restore_inspection_only(path, &FixtureKeys, pin(fixture.alice_signer.public()))
            .unwrap();
    let live = std::mem::replace(&mut fixture.core, placeholder);
    (Sdk::bind_native(live, binding(), NOW).unwrap(), fixture)
}
#[test]
fn protected_chat_actual_peer_decrypts_bound_inner_envelope_and_pending_is_identical() {
    let dir = run_dir();
    let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("client.sqlite"));
    assert!(sdk.matches_native_actor("profile", "main", "session"));
    assert!(!sdk.matches_native_actor("profile", "other", "session"));
    let wire = sdk.send_chat(EVENT, "fixture protected body", NOW).unwrap();
    assert_eq!(
        sdk.pending_chat_for_native_publish(EVENT, NOW)
            .unwrap()
            .unwrap(),
        wire
    );
    let plaintext = peer.decrypt(&wire);
    let value: Value = coset::cbor::de::from_reader(plaintext.as_slice()).unwrap();
    let Value::Array(fields) = value else {
        panic!("fixture envelope")
    };
    assert_eq!(fields[0], Value::Text("MnemaTalk ProtectedChat".into()));
    assert_eq!(fields[4], Value::Text(CHANNEL.into()));
    assert_eq!(fields[6], Value::Text("bob".into()));
    assert_eq!(fields[7], Value::Text("desktop".into()));
    assert_eq!(fields[8], Value::Text(EVENT.into()));
    assert_eq!(fields[9], Value::Text("fixture protected body".into()));
    sdk.retire_native();
    assert!(sdk.pending_chat_for_native_publish(EVENT, NOW).is_err());
    assert!(
        sdk.send_chat("44444444-4444-4444-8444-444444444444", "blocked", NOW)
            .is_err()
    );
}
fn peer_envelope(domain: &str, event: &str) -> Value {
    Value::Array(vec![
        Value::Text(domain.into()),
        Value::Integer(1.into()),
        Value::Text(scope().origin().into()),
        Value::Text(scope().community().into()),
        Value::Text(CHANNEL.into()),
        Value::Bytes(GROUP.into()),
        Value::Text("alice".into()),
        Value::Text("desktop".into()),
        Value::Text(event.into()),
        Value::Text("authenticated peer chat".into()),
    ])
}
#[test]
fn actual_mls_sender_must_match_inner_author_and_native_route() {
    for field in [0usize, 1, 2, 3, 4, 5, 6, 7, 8] {
        let dir = run_dir();
        let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("client.sqlite"));
        let mut value = peer_envelope("MnemaTalk ProtectedChat", EVENT);
        let Value::Array(fields) = &mut value else {
            panic!("fixture envelope")
        };
        fields[field] = match field {
            1 => Value::Integer(2.into()),
            5 => Value::Bytes(b"wrong-group".to_vec()),
            _ => Value::Text("wrong-field".into()),
        };
        let wire = peer.peer_message(&encode(&value));
        assert!(
            sdk.receive_chat(EVENT, &wire, NOW).is_err(),
            "field {field}"
        );
        assert!(!sdk.matches_native_actor("profile", "main", "session"));
    }
    let dir = run_dir();
    let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("client.sqlite"));
    let wire = peer.peer_message(&encode(&peer_envelope("MnemaTalk ProtectedChat", EVENT)));
    let received = sdk.receive_chat(EVENT, &wire, NOW).unwrap();
    assert_eq!(received.account, "alice");
    assert_eq!(received.device, "desktop");
    assert_eq!(received.body, "authenticated peer chat");
}
#[test]
fn malformed_authenticated_inner_data_never_panics_or_returns_plaintext() {
    for payload in [
        vec![0x81; 2048],
        vec![0x9b, 255, 255, 255, 255, 255, 255, 255, 255],
        vec![0x9f, 0xff],
        vec![0; 32769],
        vec![0x80],
    ] {
        let dir = run_dir();
        let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("client.sqlite"));
        let wire = peer.peer_message(&payload);
        assert!(sdk.receive_chat(EVENT, &wire, NOW).is_err());
    }
}
#[test]
fn suppressed_chat_outbox_and_metadata_inserts_never_return_ciphertext() {
    for table in ["core_outbox", "sdk_events"] {
        let dir = run_dir();
        let (mut sdk, _) = sdk_fixture(&dir.path().join("client.sqlite"));
        let before = secret(&sdk.core.connection);
        sdk.core
            .connection
            .execute_batch(&format!(
                "CREATE TRIGGER suppress BEFORE INSERT ON {table} BEGIN SELECT RAISE(IGNORE);END;"
            ))
            .unwrap();
        assert_eq!(
            sdk.send_chat(EVENT, "fault fixture", NOW),
            Err(Error::Database)
        );
        assert_eq!(secret(&sdk.core.connection), before);
        assert_eq!(sdk.core.phase(), Phase::Quarantined);
        assert_eq!(count(&sdk.core.connection, "core_outbox"), 0);
        assert_eq!(count(&sdk.core.connection, "sdk_events"), 0);
    }
}
#[test]
fn real_epoch_exporter_and_durable_unique_context_match_peer_standard_mapping() {
    let dir = run_dir();
    let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("client.sqlite"));
    let lease = sdk
        .reserve_source(EVENT, SOURCE, SourceKind::Opus, NOW)
        .unwrap();
    let plaintext = peer.decrypt(lease.announcement_for_native_relay());
    let value: Value = coset::cbor::de::from_reader(plaintext.as_slice()).unwrap();
    let Value::Array(fields) = value else {
        panic!("source fixture")
    };
    assert_eq!(fields[0], Value::Text("MnemaTalk ProtectedSource".into()));
    let expected = peer
        .alice_group
        .export_secret(peer.alice_provider.crypto(), "SFrame 1.0 Base Key", b"", 32)
        .unwrap();
    let material = sdk.consume_for_native_worker(lease, NOW).unwrap();
    assert_eq!(material.fixture_key(), expected);
    let epoch = peer.alice_group.epoch().as_u64();
    assert_eq!(
        material.key_id_for_native(),
        (1u64 << 48) | (1u64 << 32) | epoch
    );
    assert!(material.sends_for_native());
    let next = sdk
        .reserve_source(
            "44444444-4444-4444-8444-444444444444",
            SOURCE,
            SourceKind::Vp8,
            NOW,
        )
        .unwrap();
    let next_material = sdk.consume_for_native_worker(next, NOW).unwrap();
    assert_eq!(next_material.fixture_key(), expected);
    assert_ne!(
        next_material.key_id_for_native(),
        material.key_id_for_native()
    );
    assert_eq!(count(&sdk.core.connection, "sdk_sources"), 2);
}
#[test]
fn source_reservation_suppression_quarantines_before_any_exporter_material() {
    let dir = run_dir();
    let (mut sdk, _) = sdk_fixture(&dir.path().join("client.sqlite"));
    sdk.core
        .connection
        .execute_batch(
            "CREATE TRIGGER suppress BEFORE INSERT ON sdk_sources BEGIN SELECT RAISE(IGNORE);END;",
        )
        .unwrap();
    assert!(matches!(
        sdk.reserve_source(EVENT, SOURCE, SourceKind::Opus, NOW),
        Err(Error::Database)
    ));
    assert_eq!(count(&sdk.core.connection, "sdk_sources"), 0);
    assert!(sdk.current(NOW).is_err());
}
#[test]
fn authenticated_source_requires_actual_mls_sender_and_source_context() {
    for spoof in [false, true] {
        let dir = run_dir();
        let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("client.sqlite"));
        let mut envelope = peer_envelope("MnemaTalk ProtectedSource", EVENT);
        let Value::Array(fields) = &mut envelope else {
            panic!("fixture source")
        };
        fields[9] = Value::Array(vec![
            Value::Integer(peer.alice_group.epoch().as_u64().into()),
            Value::Integer((if spoof { 1u64 } else { 0u64 }).into()),
            Value::Integer(1.into()),
            Value::Text(SOURCE.into()),
            Value::Text("opus".into()),
        ]);
        let wire = peer.peer_message(&encode(&envelope));
        if spoof {
            assert!(sdk.receive_source(EVENT, &wire, NOW).is_err());
        } else {
            let lease = sdk.receive_source(EVENT, &wire, NOW).unwrap();
            let material = sdk.consume_for_native_worker(lease, NOW).unwrap();
            assert!(!material.sends_for_native());
            assert_eq!(
                material.key_id_for_native(),
                (1u64 << 48) | peer.alice_group.epoch().as_u64()
            );
            let expected = peer
                .alice_group
                .export_secret(peer.alice_provider.crypto(), "SFrame 1.0 Base Key", b"", 32)
                .unwrap();
            assert_eq!(material.fixture_key(), expected);
        }
    }
}

#[test]
fn source_lease_is_owner_bound_single_use_and_revoked_by_epoch_or_roster_change() {
    let dir = run_dir();
    let (mut first, _) = sdk_fixture(&dir.path().join("first.sqlite"));
    let lease = first
        .reserve_source(EVENT, SOURCE, SourceKind::Opus, NOW)
        .unwrap();
    let dir2 = run_dir();
    let (mut second, _) = sdk_fixture(&dir2.path().join("second.sqlite"));
    assert!(matches!(
        second.consume_for_native_worker(lease, NOW),
        Err(Error::Stale)
    ));
    let lease = first
        .reserve_source(
            "44444444-4444-4444-8444-444444444444",
            SOURCE,
            SourceKind::Opus,
            NOW,
        )
        .unwrap();
    let forged_duplicate = crate::protected::SourceLease::fixture_duplicate(&lease);
    first.consume_for_native_worker(lease, NOW).unwrap();
    assert!(matches!(
        first.consume_for_native_worker(forged_duplicate, NOW),
        Err(Error::Replay)
    ));
    let lease = first
        .reserve_source(
            "55555555-5555-4555-8555-555555555555",
            SOURCE,
            SourceKind::Opus,
            NOW,
        )
        .unwrap();
    first.retire_native();
    assert!(first.consume_for_native_worker(lease, NOW).is_err());
    let dir3 = run_dir();
    let (mut third, mut peer) = sdk_fixture(&dir3.path().join("third.sqlite"));
    let lease = third
        .reserve_source(EVENT, SOURCE, SourceKind::Opus, NOW)
        .unwrap();
    let commit = peer.self_update();
    let handle = third.core.stage_commit(&commit, NOW).unwrap();
    third.core.inspect_stage(handle).unwrap();
    let authorized = third.core.authorize_stage(handle, NOW).unwrap();
    third
        .core
        .merge_authorized(authorized, "native-merge", NOW)
        .unwrap();
    assert!(matches!(
        third.consume_for_native_worker(lease, NOW),
        Err(Error::Stale)
    ));
}

fn peer_sframe_encrypt(
    base: &[u8],
    kid: u64,
    counter: u64,
    prefix: &[u8],
    payload: &[u8],
) -> Vec<u8> {
    use sframe::{
        CipherSuite, crypto::EncryptionBufferView, header::SframeHeader, key::EncryptionKey,
    };
    let key = EncryptionKey::derive_from(CipherSuite::AesGcm256Sha512, kid, base).unwrap();
    let header = SframeHeader::new(kid, counter);
    let mut serialized = vec![0; header.len()];
    header.serialize(&mut serialized).unwrap();
    let mut aad = [serialized.as_slice(), prefix].concat();
    let mut data = payload.to_vec();
    let mut tag = vec![0; 16];
    key.encrypt(
        EncryptionBufferView {
            aad: &mut aad,
            data: &mut data,
            tag: &mut tag,
        },
        counter,
    )
    .unwrap();
    [
        prefix,
        serialized.as_slice(),
        data.as_slice(),
        tag.as_slice(),
    ]
    .concat()
}
#[test]
fn native_key_only_frame_bridge_matches_real_mls_peer_and_preserves_counter_replay() {
    use sframe::{
        CipherSuite, crypto::DecryptionBufferView, header::SframeHeader, key::DecryptionKey,
    };
    let dir = run_dir();
    let (mut sdk, peer) = sdk_fixture(&dir.path().join("client.sqlite"));
    let lease = sdk
        .reserve_source(EVENT, SOURCE, SourceKind::Opus, NOW)
        .unwrap();
    let send = sdk.activate_native_frame_bridge(lease, NOW).unwrap();
    let plain = b"\x01fixture native opus packet";
    let first = sdk.transform_native_frame(&send, plain, NOW).unwrap();
    let second = sdk.transform_native_frame(&send, plain, NOW).unwrap();
    let header = SframeHeader::deserialize(&first[1..]).unwrap();
    let header2 = SframeHeader::deserialize(&second[1..]).unwrap();
    assert_eq!(header.counter(), 0);
    assert_eq!(header2.counter(), 1);
    assert_ne!(first, second);
    let base = peer
        .alice_group
        .export_secret(peer.alice_provider.crypto(), "SFrame 1.0 Base Key", b"", 32)
        .unwrap();
    let key =
        DecryptionKey::derive_from(CipherSuite::AesGcm256Sha512, header.key_id(), &base).unwrap();
    let mut aad = [&first[1..1 + header.len()], &first[..1]].concat();
    let mut data = first[1 + header.len()..].to_vec();
    key.decrypt(
        DecryptionBufferView {
            aad: &mut aad,
            data: &mut data,
        },
        header.counter(),
    )
    .unwrap();
    data.truncate(data.len() - 16);
    assert_eq!([&first[..1], data.as_slice()].concat(), plain);
    let mut peer = peer;
    let recv_event = "44444444-4444-4444-8444-444444444444";
    let mut envelope = peer_envelope("MnemaTalk ProtectedSource", recv_event);
    let Value::Array(fields) = &mut envelope else {
        panic!("fixture source")
    };
    let epoch = peer.alice_group.epoch().as_u64();
    fields[9] = Value::Array(vec![
        Value::Integer(epoch.into()),
        Value::Integer(0.into()),
        Value::Integer(1.into()),
        Value::Text(SOURCE.into()),
        Value::Text("opus".into()),
    ]);
    let source_wire = peer.peer_message(&encode(&envelope));
    let lease = sdk.receive_source(recv_event, &source_wire, NOW).unwrap();
    let recv = sdk.activate_native_frame_bridge(lease, NOW).unwrap();
    let encrypted = peer_sframe_encrypt(&base, (1u64 << 48) | epoch, 0, &plain[..1], &plain[1..]);
    for point in [0, encrypted.len() - 1] {
        let mut tampered = encrypted.clone();
        tampered[point] ^= 1;
        assert!(sdk.transform_native_frame(&recv, &tampered, NOW).is_err());
    }
    for end in 0..encrypted.len() {
        assert!(
            sdk.transform_native_frame(&recv, &encrypted[..end], NOW)
                .is_err()
        );
    }
    assert_eq!(
        sdk.transform_native_frame(&recv, &encrypted, NOW).unwrap(),
        plain
    );
    assert_eq!(
        sdk.transform_native_frame(&recv, &encrypted, NOW),
        Err(Error::Replay)
    );
    let other_context =
        peer_sframe_encrypt(&base, (2u64 << 48) | epoch, 1, &plain[..1], &plain[1..]);
    assert_eq!(
        sdk.transform_native_frame(&recv, &other_context, NOW),
        Err(Error::Unauthorized)
    );
    sdk.stop_native_frame_bridge(send).unwrap();
    sdk.stop_native_frame_bridge(recv).unwrap();
    assert!(sdk.bridges_fixture_count() == 0);
}

#[test]
fn native_frame_material_clock_commit_and_context_commit_faults_fail_closed() {
    let dir = run_dir();
    let (mut sdk, _) = sdk_fixture(&dir.path().join("client.sqlite"));
    let lease = sdk
        .reserve_source(EVENT, SOURCE, SourceKind::Opus, NOW)
        .unwrap();
    sdk.activate_native_frame_bridge(lease, NOW + 200).unwrap();
    assert!(
        sdk.reserve_source(
            "44444444-4444-4444-8444-444444444444",
            SOURCE,
            SourceKind::Opus,
            NOW + 100
        )
        .is_err()
    );
    let dir = run_dir();
    let (mut sdk, _) = sdk_fixture(&dir.path().join("client.sqlite"));
    sdk.core.connection.execute_batch("CREATE TABLE fixture_parent(id INTEGER PRIMARY KEY);CREATE TABLE fixture_child(id INTEGER REFERENCES fixture_parent(id) DEFERRABLE INITIALLY DEFERRED);CREATE TRIGGER fail_commit AFTER INSERT ON sdk_sources BEGIN INSERT INTO fixture_child VALUES(1);END;").unwrap();
    assert!(matches!(
        sdk.reserve_source(EVENT, SOURCE, SourceKind::Opus, NOW),
        Err(Error::Database)
    ));
    assert_eq!(count(&sdk.core.connection, "sdk_sources"), 0);
    assert_eq!(count(&sdk.core.connection, "core_outbox"), 0);
    assert!(sdk.current(NOW).is_err());
}

#[test]
fn native_frame_and_pending_read_clock_cannot_rewind_or_reactivate_expired_roster() {
    let dir = run_dir();
    let (mut sdk, _) = sdk_fixture(&dir.path().join("client.sqlite"));
    let lease = sdk
        .reserve_source(EVENT, SOURCE, SourceKind::Opus, NOW)
        .unwrap();
    let grant = sdk.activate_native_frame_bridge(lease, NOW).unwrap();
    sdk.transform_native_frame(&grant, b"\x01native fixture", NOW + 200)
        .unwrap();
    assert_eq!(
        sdk.transform_native_frame(&grant, b"\x01backward fixture", NOW + 100),
        Err(Error::Replay)
    );
    sdk.pending_chat_for_native_publish(EVENT, NOW + 300)
        .unwrap();
    assert_eq!(
        sdk.transform_native_frame(&grant, b"\x01backward after pending", NOW + 250),
        Err(Error::Replay)
    );
    assert!(
        sdk.transform_native_frame(&grant, b"\x01expired", NOW + 1000)
            .is_err()
    );
    assert_eq!(
        sdk.transform_native_frame(&grant, b"\x01expired cannot revive", NOW + 900),
        Err(Error::Replay)
    );
}

#[test]
fn native_actor_account_binding_and_roster_update_burn_existing_media_grants() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("client.sqlite"));
    f.live();
    let incorrect =
        NativeBinding::from_native_actor(CHANNEL, "profile", "main", "session", "alice", "desktop")
            .unwrap();
    assert!(matches!(
        Sdk::bind_native(f.core, incorrect, NOW),
        Err(Error::Unauthorized)
    ));
    let dir = run_dir();
    let (mut sdk, peer) = sdk_fixture(&dir.path().join("client.sqlite"));
    let lease = sdk
        .reserve_source(EVENT, SOURCE, SourceKind::Opus, NOW)
        .unwrap();
    let grant = sdk.activate_native_frame_bridge(lease, NOW).unwrap();
    let update = roster(
        2,
        vec![
            ("alice", "desktop", b"alice", peer.alice_signer.public()),
            ("bob", "desktop", b"bob", &peer.offer.signature_key),
        ],
    );
    sdk.install_native_roster(&update, NOW).unwrap();
    assert_eq!(
        sdk.transform_native_frame(&grant, b"\x01old grant", NOW),
        Err(Error::Stale)
    );
    assert!(sdk.pending_chat_for_native_publish(EVENT, NOW).is_err());
    let lease = sdk
        .reserve_source(
            "44444444-4444-4444-8444-444444444444",
            SOURCE,
            SourceKind::Opus,
            NOW,
        )
        .unwrap();
    let material = sdk.consume_for_native_worker(lease, NOW).unwrap();
    assert_eq!(material.key_id_for_native() >> 48, 2);
}

#[test]
fn native_lowlevel_exact_rfc9605_vector_uses_header_then_metadata_aad() {
    let base: Vec<u8> = (0u8..16).collect();
    let encoded = peer_sframe_encrypt(
        &base,
        0x123,
        0x4567,
        b"IETF SFrame WG",
        b"draft-ietf-sframe-enc",
    );
    let expected =
        "990123456794f509d36e9beacb0e261d99c7d1e972f1fed787d4049f17ca21353c1cc24d56ceabced279";
    let got: String = encoded[b"IETF SFrame WG".len()..]
        .iter()
        .map(|b| format!("{b:02x}"))
        .collect();
    assert_eq!(got, expected);
}

#[test]
fn actual_source_context_exit_helper() {
    let Ok(path) = std::env::var("MNEMA_SDK_SOURCE_CHILD_DB") else {
        return;
    };
    let path = PathBuf::from(path);
    assert!(path.starts_with(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("evidence/runs")));
    let (mut sdk, _) = sdk_fixture(&path);
    sdk.reserve_source(EVENT, SOURCE, SourceKind::Opus, NOW)
        .unwrap();
    panic!("fixture exit boundary was not reached");
}

#[test]
fn three_actual_source_context_process_exits_never_enable_restored_sender() {
    for (point, contexts, outbox) in [
        ("before-context-commit", 0, 0),
        ("after-context-commit", 1, 0),
        ("after-announcement-commit", 1, 1),
    ] {
        let dir = run_dir();
        let path = dir.path().join("source.sqlite");
        let child = std::process::Command::new(std::env::current_exe().unwrap())
            .args([
                "--exact",
                "tests::actual_source_context_exit_helper",
                "--nocapture",
            ])
            .env("MNEMA_SDK_SOURCE_CHILD_DB", &path)
            .env("MNEMA_SDK_SOURCE_FAULT", point)
            .status()
            .unwrap();
        assert_eq!(child.code(), Some(73));
        let connection = open_encrypted(&path, &FixtureKeys, false).unwrap();
        assert_eq!(count(&connection, "sdk_sources"), contexts);
        assert_eq!(count(&connection, "core_outbox"), outbox);
        assert_eq!(count(&connection, "sdk_events"), outbox);
        let peer: Vec<u8> = connection
            .query_row("SELECT peer_key FROM core_pin", [], |r| r.get(0))
            .unwrap();
        drop(connection);
        let restored = Core::restore_inspection_only(&path, &FixtureKeys, pin(&peer)).unwrap();
        assert_eq!(restored.phase(), Phase::Restored);
        assert!(matches!(
            Sdk::bind_native(restored, binding(), NOW),
            Err(Error::Restored)
        ));
    }
}

#[cfg(target_os = "macos")]
#[test]
fn actual_isolated_keychain_device_possession_is_the_live_core_offer_and_media_exporter() {
    use crate::native_fixture::{NoUi, OwnedFixture, serial, snapshot};
    use mnema_crypto_enrollment_prototype::{Device, Issuer, NativeAdmissionIntent};
    let _serial = serial().unwrap();
    let _ui = NoUi::begin().unwrap();
    let before = snapshot().unwrap();
    let root_dir = run_dir();
    let alice_dir = run_dir();
    let bob_dir = run_dir();
    let mut root = OwnedFixture::create(root_dir.path()).unwrap();
    let mut alice = OwnedFixture::create(alice_dir.path()).unwrap();
    let mut bob = OwnedFixture::create(bob_dir.path()).unwrap();
    assert!(root.path.exists() && alice.path.exists() && bob.path.exists());
    {
        let mut issuer = Issuer::provision_fresh(
            &root_dir.path().join("issuer.sqlite"),
            &root.vault,
            scope(),
            GROUP,
        )
        .unwrap();
        let native_pin = issuer.pin_for_native_out_of_band_transfer();
        let alice_device = Device::provision_native(&alice.vault, native_pin.clone()).unwrap();
        let bob_device = Device::provision_native(&bob.vault, native_pin.clone()).unwrap();
        bob.vault.create_seed("database-key").unwrap();
        let invite_alice = issuer
            .invite_native_reviewed_device(
                NativeAdmissionIntent::from_native_out_of_band_pin(
                    "alice",
                    "desktop",
                    b"alice",
                    alice_device.public_key(),
                )
                .unwrap(),
                NOW,
            )
            .unwrap();
        issuer
            .admit_proven_device(&alice_device.respond(&invite_alice, NOW).unwrap(), NOW)
            .unwrap();
        let peer_key = alice_device.public_key();
        let core_pin = NativeBootstrap::from_native_pin(
            scope(),
            GROUP,
            native_pin.authority_key(),
            b"alice",
            peer_key,
        )
        .unwrap();
        bob.lock().unwrap();
        assert!(matches!(
            Core::begin_native_enrollment(
                &bob_dir.path().join("must-not-create.sqlite"),
                &bob.vault,
                core_pin,
                b"bob"
            ),
            Err(Error::KeyUnavailable)
        ));
        assert!(!bob_dir.path().join("must-not-create.sqlite").exists());
        bob.unlock().unwrap();
        let core_pin = NativeBootstrap::from_native_pin(
            scope(),
            GROUP,
            native_pin.authority_key(),
            b"alice",
            peer_key,
        )
        .unwrap();
        let (mut core, offer) = Core::begin_native_enrollment(
            &bob_dir.path().join("client.sqlite"),
            &bob.vault,
            core_pin,
            b"bob",
        )
        .unwrap();
        assert_eq!(offer.signature_key, bob_device.public_key());
        let provider = OpenMlsRustCrypto::default();
        let package_wire = offer.key_package.tls_serialize_detached().unwrap();
        let validated = KeyPackageIn::tls_deserialize_exact(&package_wire)
            .unwrap()
            .validate(provider.crypto(), ProtocolVersion::Mls10)
            .unwrap();
        assert_eq!(
            validated.leaf_node().signature_key().as_slice(),
            bob_device.public_key()
        );
        let invite_bob = issuer
            .invite_native_reviewed_device(
                NativeAdmissionIntent::from_native_out_of_band_pin(
                    "bob",
                    "desktop",
                    b"bob",
                    bob_device.public_key(),
                )
                .unwrap(),
                NOW,
            )
            .unwrap();
        let response = bob_device.respond(&invite_bob, NOW).unwrap();
        let actual_roster = issuer.admit_proven_device(&response, NOW).unwrap();
        core.install_roster(&actual_roster, NOW).unwrap();
        let alice_seed = alice.vault.read_seed("device-key").unwrap();
        let signer = SignatureKeyPair::from_raw(
            SignatureScheme::ED25519,
            alice_seed.to_vec(),
            peer_key.to_vec(),
        );
        signer.store(provider.storage()).unwrap();
        let config = MlsGroupCreateConfig::builder()
            .ciphersuite(SUITE)
            .use_ratchet_tree_extension(true)
            .build();
        let mut group = MlsGroup::new_with_group_id(
            &provider,
            &signer,
            &config,
            GroupId::from_slice(GROUP),
            CredentialWithKey {
                credential: BasicCredential::new(b"alice".to_vec()).into(),
                signature_key: peer_key.to_vec().into(),
            },
        )
        .unwrap();
        let (_, welcome, _) = group.add_members(&provider, &signer, &[validated]).unwrap();
        group.merge_pending_commit(&provider).unwrap();
        core.accept_welcome(&welcome.to_bytes().unwrap(), NOW)
            .unwrap();
        let proof = core.peer_challenge_transcript().unwrap();
        let protected = group
            .create_message(&provider, &signer, &proof)
            .unwrap()
            .to_bytes()
            .unwrap();
        core.confirm_peer(&protected, NOW).unwrap();
        let mut sdk = Sdk::bind_native(core, binding(), NOW).unwrap();
        let chat = sdk
            .send_chat(EVENT, "actual native fixture fusion", NOW)
            .unwrap();
        let processed = group
            .process_message(
                &provider,
                MlsMessageIn::tls_deserialize_exact(&chat)
                    .unwrap()
                    .try_into_protocol_message()
                    .unwrap(),
            )
            .unwrap();
        assert_eq!(processed.credential().serialized_content(), b"bob");
        let ProcessedMessageContent::ApplicationMessage(app) = processed.into_content() else {
            panic!("native fixture chat")
        };
        let value: Value = coset::cbor::de::from_reader(app.into_bytes().as_slice()).unwrap();
        let Value::Array(fields) = value else {
            panic!("native fixture envelope")
        };
        assert_eq!(fields[6], Value::Text("bob".into()));
        let lease = sdk
            .reserve_source(
                "44444444-4444-4444-8444-444444444444",
                SOURCE,
                SourceKind::Opus,
                NOW,
            )
            .unwrap();
        let material = sdk.consume_for_native_worker(lease, NOW).unwrap();
        assert_eq!(
            material.fixture_key(),
            group
                .export_secret(provider.crypto(), "SFrame 1.0 Base Key", b"", 32)
                .unwrap()
        );
        sdk.retire_native();
    }
    root.delete().unwrap();
    alice.delete().unwrap();
    bob.delete().unwrap();
    assert!(before == snapshot().unwrap());
}
impl Fixture {
    fn offered(path: &Path) -> Self {
        let alice_provider = OpenMlsRustCrypto::default();
        let alice_signer = SignatureKeyPair::new(SignatureScheme::ED25519).unwrap();
        alice_signer.store(alice_provider.storage()).unwrap();
        let config = MlsGroupCreateConfig::builder()
            .ciphersuite(SUITE)
            .use_ratchet_tree_extension(true)
            .build();
        let mut alice_group = MlsGroup::new_with_group_id(
            &alice_provider,
            &alice_signer,
            &config,
            GroupId::from_slice(GROUP),
            CredentialWithKey {
                credential: BasicCredential::new(b"alice".to_vec()).into(),
                signature_key: alice_signer.to_public_vec().into(),
            },
        )
        .unwrap();
        let (core, offer) = Core::begin_enrollment(
            path,
            &FixtureKeys,
            pin(&alice_signer.to_public_vec()),
            b"bob",
        )
        .unwrap();
        let (_, welcome, _) = alice_group
            .add_members(
                &alice_provider,
                &alice_signer,
                std::slice::from_ref(&offer.key_package),
            )
            .unwrap();
        alice_group.merge_pending_commit(&alice_provider).unwrap();
        Self {
            core,
            alice_provider,
            alice_group,
            alice_signer,
            offer,
            welcome: welcome.to_bytes().unwrap(),
        }
    }
    fn entries(&self) -> Vec<(&str, &str, &[u8], &[u8])> {
        vec![
            ("alice", "desktop", b"alice", self.alice_signer.public()),
            ("bob", "desktop", b"bob", &self.offer.signature_key),
        ]
    }
    fn initial_roster(&self) -> Vec<u8> {
        roster(1, self.entries())
    }
    fn awaiting(&mut self) {
        self.core
            .install_roster(&self.initial_roster(), NOW)
            .unwrap();
        self.core.accept_welcome(&self.welcome, NOW).unwrap();
    }
    fn peer_message(&mut self, plaintext: &[u8]) -> Vec<u8> {
        self.alice_group
            .create_message(&self.alice_provider, &self.alice_signer, plaintext)
            .unwrap()
            .to_bytes()
            .unwrap()
    }
    fn live(&mut self) {
        self.awaiting();
        let transcript = self.core.peer_challenge_transcript().unwrap();
        let wire = self.peer_message(&transcript);
        self.core.confirm_peer(&wire, NOW).unwrap();
    }
    fn decrypt(&mut self, wire: &[u8]) -> Vec<u8> {
        let message = MlsMessageIn::tls_deserialize_exact(wire)
            .unwrap()
            .try_into_protocol_message()
            .unwrap();
        let processed = self
            .alice_group
            .process_message(&self.alice_provider, message)
            .unwrap();
        let ProcessedMessageContent::ApplicationMessage(app) = processed.into_content() else {
            panic!("fixture application");
        };
        app.into_bytes()
    }
    fn self_update(&mut self) -> Vec<u8> {
        let messages = self
            .alice_group
            .self_update(
                &self.alice_provider,
                &self.alice_signer,
                LeafNodeParameters::default(),
            )
            .unwrap();
        let wire = messages.commit().to_bytes().unwrap();
        self.alice_group
            .merge_pending_commit(&self.alice_provider)
            .unwrap();
        wire
    }
}
fn secret(conn: &Connection) -> Vec<u8> {
    conn.query_row(
        "SELECT group_data FROM openmls_group_data WHERE data_type='message_secrets'",
        [],
        |r| r.get(0),
    )
    .unwrap()
}
fn count(conn: &Connection, table: &str) -> i64 {
    conn.query_row(&format!("SELECT COUNT(*) FROM {table}"), [], |r| r.get(0))
        .unwrap()
}
#[test]
fn actual_sqlcipher_encryption_hmac_wrong_key_locked_key_and_pin() {
    let dir = run_dir();
    let path = dir.path().join("state.sqlite");
    let mut f = Fixture::offered(&path);
    f.live();
    let version: String = f
        .core
        .connection
        .pragma_query_value(None, "cipher_version", |r| r.get(0))
        .unwrap();
    assert!(version.starts_with("4."));
    assert_eq!(
        f.core
            .connection
            .pragma_query_value(None, "synchronous", |r| r.get::<_, i64>(0))
            .unwrap(),
        3
    );
    let mut statement = f
        .core
        .connection
        .prepare("PRAGMA cipher_integrity_check")
        .unwrap();
    assert!(statement.query([]).unwrap().next().unwrap().is_none());
    drop(statement);
    let bytes = std::fs::read(&path).unwrap();
    assert!(!bytes.starts_with(b"SQLite format 3"));
    assert!(!bytes.windows(TEXT.len()).any(|w| w == TEXT));
    assert!(!bytes.windows(GROUP.len()).any(|w| w == GROUP));
    assert!(matches!(
        Core::restore_inspection_only(&path, &Wrong, pin(f.alice_signer.public())),
        Err(Error::Database)
    ));
    assert!(matches!(
        Core::restore_inspection_only(&path, &Locked, pin(f.alice_signer.public())),
        Err(Error::KeyUnavailable)
    ));
    let mut wrong_pin = pin(f.alice_signer.public());
    wrong_pin.scope = Scope::new("https://other.example", "community").unwrap();
    assert!(matches!(
        Core::restore_inspection_only(&path, &FixtureKeys, wrong_pin),
        Err(Error::Trust)
    ));
}
#[test]
fn tampered_sqlcipher_page_fails_authentication() {
    let dir = run_dir();
    let path = dir.path().join("state.sqlite");
    let mut f = Fixture::offered(&path);
    f.live();
    let peer = f.alice_signer.to_public_vec();
    drop(f);
    let mut bytes = std::fs::read(&path).unwrap();
    bytes[128] ^= 1;
    std::fs::write(&path, bytes).unwrap();
    assert!(matches!(
        Core::restore_inspection_only(&path, &FixtureKeys, pin(&peer)),
        Err(Error::Database)
    ));
}
#[test]
fn no_boolean_activation_real_peer_ratchets_and_identical_outbox() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    assert_eq!(f.core.send("early", TEXT, NOW), Err(Error::WrongPhase));
    f.awaiting();
    assert_eq!(f.core.send("early", TEXT, NOW), Err(Error::WrongPhase));
    let before = secret(&f.core.connection);
    let transcript = f.core.peer_challenge_transcript().unwrap();
    let wire = f.peer_message(&transcript);
    f.core.confirm_peer(&wire, NOW).unwrap();
    assert!(secret(&f.core.connection) != before);
    let first = f.core.send("first", TEXT, NOW).unwrap();
    assert_eq!(f.core.pending("first").unwrap(), Some(first.clone()));
    assert_eq!(f.decrypt(&first), TEXT);
    let second = f.core.send("second", b"next fixture", NOW + 1).unwrap();
    assert_ne!(first, second);
    assert_eq!(f.decrypt(&second), b"next fixture");
    let before = secret(&f.core.connection);
    assert_eq!(
        f.core.send("first", b"changed retry", NOW + 1),
        Err(Error::Replay)
    );
    assert_eq!(secret(&f.core.connection), before);
    assert_eq!(f.core.phase(), Phase::Quarantined);
    assert_eq!(f.core.pending("first").unwrap(), Some(first));
}
#[test]
fn restored_eligibility_row_and_old_snapshot_never_enable_sending() {
    let dir = run_dir();
    let path = dir.path().join("state.sqlite");
    let mut f = Fixture::offered(&path);
    f.live();
    let original = f.core.send("first", TEXT, NOW).unwrap();
    let peer = f.alice_signer.to_public_vec();
    drop(f.core);
    let mut restored = Core::restore_inspection_only(&path, &FixtureKeys, pin(&peer)).unwrap();
    assert_eq!(restored.phase(), Phase::Restored);
    assert_eq!(restored.send("new", TEXT, NOW), Err(Error::Restored));
    assert_eq!(restored.install_roster(b"", NOW), Err(Error::Restored));
    assert_eq!(
        restored.accept_welcome(&f.welcome, NOW),
        Err(Error::Restored)
    );
    assert_eq!(restored.confirm_peer(b"", NOW), Err(Error::Restored));
    assert_eq!(restored.pending("first").unwrap(), Some(original));
}
#[test]
fn fresh_device_rejoin_cannot_use_old_welcome_or_old_challenge() {
    let dir = run_dir();
    let mut old = Fixture::offered(&dir.path().join("old.sqlite"));
    old.awaiting();
    let old_transcript = old.core.peer_challenge_transcript().unwrap();
    let old_response = old.peer_message(&old_transcript);
    let (mut fresh, offer) = Core::begin_enrollment(
        &dir.path().join("fresh.sqlite"),
        &FixtureKeys,
        pin(old.alice_signer.public()),
        b"bob-fresh",
    )
    .unwrap();
    assert_ne!(offer.signature_key, old.offer.signature_key);
    assert_ne!(offer.challenge, old.offer.challenge);
    let signed = roster(
        2,
        vec![
            ("alice", "desktop", b"alice", old.alice_signer.public()),
            ("bob", "fresh", b"bob-fresh", &offer.signature_key),
        ],
    );
    fresh.install_roster(&signed, NOW).unwrap();
    assert_eq!(
        fresh.accept_welcome(&old.welcome, NOW),
        Err(Error::Provider)
    );
    assert_eq!(fresh.phase(), Phase::Quarantined);
    let dir2 = run_dir();
    let mut other = Fixture::offered(&dir2.path().join("other.sqlite"));
    other.awaiting();
    assert_eq!(
        other.core.confirm_peer(&old_response, NOW),
        Err(Error::Provider)
    );
    assert_eq!(other.core.phase(), Phase::Quarantined);
}
#[test]
fn wrong_authenticated_peer_transcript_never_activates() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.awaiting();
    let mut transcript = f.core.peer_challenge_transcript().unwrap();
    *transcript.last_mut().unwrap() ^= 1;
    let wire = f.peer_message(&transcript);
    let before = secret(&f.core.connection);
    assert_eq!(f.core.confirm_peer(&wire, NOW), Err(Error::Unauthorized));
    assert_eq!(secret(&f.core.connection), before);
    assert_eq!(f.core.phase(), Phase::Quarantined);
    assert!(
        f.core
            .connection
            .query_row("SELECT fresh_join_epoch IS NULL FROM core_state", [], |r| r
                .get::<_, bool>(0))
            .unwrap()
    );
}
#[test]
fn unknown_welcome_recipient_is_rejected_before_group_install() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    let signed = roster(
        1,
        vec![("alice", "desktop", b"alice", f.alice_signer.public())],
    );
    f.core.install_roster(&signed, NOW).unwrap();
    assert_eq!(
        f.core.accept_welcome(&f.welcome, NOW),
        Err(Error::Unauthorized)
    );
    let provider = Provider::new(&f.core.connection, &f.core.crypto);
    assert!(
        MlsGroup::load(provider.storage(), &GroupId::from_slice(GROUP))
            .unwrap()
            .is_none()
    );
    assert_eq!(f.core.phase(), Phase::Quarantined);
}
#[test]
fn signed_floor_time_and_wire_commit_atomically_or_quarantine() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.core.connection.execute_batch("CREATE TRIGGER fail_roster BEFORE UPDATE OF roster ON core_state BEGIN SELECT RAISE(ABORT,'fixture');END;").unwrap();
    assert_eq!(
        f.core.install_roster(&f.initial_roster(), NOW),
        Err(Error::Database)
    );
    assert_eq!(stored_floor(&f.core.connection).unwrap(), (0, 0));
    assert!(
        f.core
            .connection
            .query_row("SELECT roster IS NULL FROM core_state", [], |r| r
                .get::<_, bool>(0))
            .unwrap()
    );
    assert_eq!(f.core.phase(), Phase::Quarantined);
}
#[test]
fn revocation_blocks_live_recipient_without_encrypting_old_tree() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.live();
    let signed = roster(2, vec![("bob", "desktop", b"bob", &f.offer.signature_key)]);
    f.core.install_roster(&signed, NOW + 1).unwrap();
    let before = secret(&f.core.connection);
    assert_eq!(
        f.core.send("revoked", TEXT, NOW + 1),
        Err(Error::Unauthorized)
    );
    assert_eq!(secret(&f.core.connection), before);
    assert_eq!(count(&f.core.connection, "core_outbox"), 0);
}
#[test]
fn wrong_api_precheck_then_same_bytes_correct_receive_and_commit() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.live();
    let app = f.peer_message(TEXT);
    let before = secret(&f.core.connection);
    assert_eq!(f.core.stage_commit(&app, NOW), Err(Error::Invalid));
    assert_eq!(f.core.phase(), Phase::Live);
    assert_eq!(secret(&f.core.connection), before);
    assert_eq!(f.core.receive_application(&app, NOW).unwrap(), TEXT);
    let commit = f.self_update();
    let before = secret(&f.core.connection);
    assert_eq!(
        f.core.receive_application(&commit, NOW),
        Err(Error::Invalid)
    );
    assert_eq!(secret(&f.core.connection), before);
    let handle = f.core.stage_commit(&commit, NOW).unwrap();
    assert!(matches!(
        f.core.authorize_stage(handle, NOW),
        Err(Error::Stale)
    ));
    let inspection = f.core.inspect_stage(handle).unwrap();
    assert_eq!(inspection.to_epoch, inspection.from_epoch + 1);
    let authorized = f.core.authorize_stage(handle, NOW).unwrap();
    f.core.merge_authorized(authorized, "accept", NOW).unwrap();
    assert_eq!(count(&f.core.connection, "core_stage"), 0);
    assert_eq!(f.core.pending("accept").unwrap(), None);
    let wire = f.core.send("post-merge", TEXT, NOW).unwrap();
    assert_eq!(f.decrypt(&wire), TEXT);
    assert!(matches!(
        f.core.authorize_stage(handle, NOW),
        Err(Error::Stale)
    ));
}
#[test]
fn unapproved_added_device_stages_but_cannot_merge() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.live();
    let provider = OpenMlsRustCrypto::default();
    let signer = SignatureKeyPair::new(SignatureScheme::ED25519).unwrap();
    let kp = KeyPackage::builder()
        .build(
            SUITE,
            &provider,
            &signer,
            CredentialWithKey {
                credential: BasicCredential::new(b"bob".to_vec()).into(),
                signature_key: signer.to_public_vec().into(),
            },
        )
        .unwrap();
    let (commit, _, _) = f
        .alice_group
        .add_members(
            &f.alice_provider,
            &f.alice_signer,
            std::slice::from_ref(kp.key_package()),
        )
        .unwrap();
    let wire = commit.to_bytes().unwrap();
    let epoch = f.core.group.as_ref().unwrap().epoch();
    let handle = f.core.stage_commit(&wire, NOW).unwrap();
    f.core.inspect_stage(handle).unwrap();
    assert!(matches!(
        f.core.authorize_stage(handle, NOW),
        Err(Error::Unauthorized)
    ));
    assert_eq!(f.core.group.as_ref().unwrap().epoch(), epoch);
    assert_eq!(count(&f.core.connection, "core_stage"), 1);
}
#[test]
fn roster_change_invalidates_authorization_before_merge() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.live();
    let wire = f.self_update();
    let handle = f.core.stage_commit(&wire, NOW).unwrap();
    f.core.inspect_stage(handle).unwrap();
    let auth = f.core.authorize_stage(handle, NOW).unwrap();
    let epoch = f.core.group.as_ref().unwrap().epoch();
    let signed = roster(2, f.entries());
    f.core.install_roster(&signed, NOW + 1).unwrap();
    assert_eq!(
        f.core.merge_authorized(auth, "accept", NOW + 1),
        Err(Error::Stale)
    );
    let provider = Provider::new(&f.core.connection, &f.core.crypto);
    assert_eq!(
        MlsGroup::load(provider.storage(), &GroupId::from_slice(GROUP))
            .unwrap()
            .unwrap()
            .epoch(),
        epoch
    );
    assert_eq!(count(&f.core.connection, "core_stage"), 1);
    assert_eq!(f.core.phase(), Phase::Quarantined);
}
#[test]
fn actual_outbox_failure_rolls_back_ratchet_and_time() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.live();
    let before = secret(&f.core.connection);
    f.core.connection.execute_batch("CREATE TRIGGER fail_outbox BEFORE INSERT ON core_outbox BEGIN SELECT RAISE(ABORT,'fixture');END;").unwrap();
    assert_eq!(f.core.send("event", TEXT, NOW + 2), Err(Error::Database));
    assert_eq!(secret(&f.core.connection), before);
    assert_eq!(stored_floor(&f.core.connection).unwrap(), (1, NOW));
    assert_eq!(count(&f.core.connection, "core_outbox"), 0);
    assert_eq!(f.core.phase(), Phase::Quarantined);
}
#[test]
fn post_commit_lost_result_quarantines_but_preserves_identical_ciphertext() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.live();
    let before = secret(&f.core.connection);
    assert_eq!(
        f.core.send_fault("event", TEXT, NOW, |point| {
            if point == FaultPoint::AfterCommit {
                Err(Error::UnknownCommit)
            } else {
                Ok(())
            }
        }),
        Err(Error::UnknownCommit)
    );
    assert_ne!(secret(&f.core.connection), before);
    assert_eq!(f.core.phase(), Phase::Quarantined);
    let wire = f.core.pending("event").unwrap().unwrap();
    assert_eq!(f.decrypt(&wire), TEXT);
    assert_eq!(f.core.send("again", TEXT, NOW), Err(Error::Quarantined));
}
#[test]
fn deferred_actual_sqlite_commit_failure_never_publishes() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.live();
    let before = secret(&f.core.connection);
    f.core.connection.execute_batch("CREATE TABLE fixture_parent(id INTEGER PRIMARY KEY);CREATE TABLE fixture_child(id INTEGER REFERENCES fixture_parent(id) DEFERRABLE INITIALLY DEFERRED);CREATE TRIGGER fail_commit AFTER INSERT ON core_outbox BEGIN INSERT INTO fixture_child VALUES(1);END;").unwrap();
    assert_eq!(f.core.send("event", TEXT, NOW), Err(Error::Database));
    assert_eq!(secret(&f.core.connection), before);
    assert_eq!(count(&f.core.connection, "core_outbox"), 0);
    assert_eq!(f.core.phase(), Phase::Quarantined);
}
#[test]
fn real_child_exit_helper() {
    let Ok(path) = std::env::var("MNEMA_CRYPTO_CORE_CHILD_PATH") else {
        return;
    };
    let point = std::env::var("MNEMA_CRYPTO_CORE_CHILD_POINT").unwrap();
    let mut f = Fixture::offered(Path::new(&path));
    f.live();
    f.core.connection.execute_batch("CREATE TABLE fixture_baseline(data BLOB);INSERT INTO fixture_baseline SELECT group_data FROM openmls_group_data WHERE data_type='message_secrets';").unwrap();
    let stop = match point.as_str() {
        "mutation" => FaultPoint::AfterMutation,
        "record" => FaultPoint::AfterRecord,
        "commit" => FaultPoint::AfterCommit,
        _ => panic!("fixture point"),
    };
    let _ = f.core.send_fault("crash-event", TEXT, NOW, |actual| {
        if actual == stop {
            std::process::exit(73);
        }
        Ok(())
    });
    panic!("fixture exit not reached");
}
#[test]
fn real_sqlcipher_abrupt_exits_at_three_transaction_boundaries() {
    for point in ["mutation", "record", "commit"] {
        let dir = run_dir();
        let path = dir.path().join("state.sqlite");
        let status = std::process::Command::new(std::env::current_exe().unwrap())
            .args(["--exact", "tests::real_child_exit_helper", "--nocapture"])
            .env("MNEMA_CRYPTO_CORE_CHILD_PATH", &path)
            .env("MNEMA_CRYPTO_CORE_CHILD_POINT", point)
            .status()
            .unwrap();
        assert_eq!(status.code(), Some(73));
        let conn = open_encrypted(&path, &FixtureKeys, false).unwrap();
        let baseline: Vec<u8> = conn
            .query_row("SELECT data FROM fixture_baseline", [], |r| r.get(0))
            .unwrap();
        let persisted = secret(&conn);
        if point == "commit" {
            assert_ne!(persisted, baseline);
            assert_eq!(count(&conn, "core_outbox"), 1);
        } else {
            assert_eq!(persisted, baseline);
            assert_eq!(count(&conn, "core_outbox"), 0);
        }
        let peer: Vec<u8> = conn
            .query_row("SELECT peer_key FROM core_pin", [], |r| r.get(0))
            .unwrap();
        drop(conn);
        let mut restored = Core::restore_inspection_only(&path, &FixtureKeys, pin(&peer)).unwrap();
        assert_eq!(
            restored.send("must-not-send", TEXT, NOW),
            Err(Error::Restored)
        );
    }
}
#[test]
fn real_existing_peer_removes_old_device_and_admits_fresh_rejoin_before_send() {
    let dir = run_dir();
    let old_path = dir.path().join("old.sqlite");
    let mut old = Fixture::offered(&old_path);
    old.live();
    let original = old.core.send("old-message", TEXT, NOW).unwrap();
    assert_eq!(old.decrypt(&original), TEXT);
    let peer_key = old.alice_signer.to_public_vec();
    drop(old.core);
    let mut restored =
        Core::restore_inspection_only(&old_path, &FixtureKeys, pin(&peer_key)).unwrap();
    let (mut fresh, offer) = Core::begin_enrollment(
        &dir.path().join("fresh.sqlite"),
        &FixtureKeys,
        pin(&peer_key),
        b"bob-fresh",
    )
    .unwrap();
    assert_ne!(offer.signature_key, old.offer.signature_key);
    let revoked = old
        .alice_group
        .members()
        .find(|m| m.credential.serialized_content() == b"bob")
        .unwrap()
        .index;
    old.alice_group
        .remove_members(&old.alice_provider, &old.alice_signer, &[revoked])
        .unwrap();
    old.alice_group
        .merge_pending_commit(&old.alice_provider)
        .unwrap();
    let (_, welcome, _) = old
        .alice_group
        .add_members(
            &old.alice_provider,
            &old.alice_signer,
            std::slice::from_ref(&offer.key_package),
        )
        .unwrap();
    old.alice_group
        .merge_pending_commit(&old.alice_provider)
        .unwrap();
    let signed = roster(
        2,
        vec![
            ("alice", "desktop", b"alice", old.alice_signer.public()),
            ("bob", "fresh", b"bob-fresh", &offer.signature_key),
        ],
    );
    fresh.install_roster(&signed, NOW + 1).unwrap();
    fresh
        .accept_welcome(&welcome.to_bytes().unwrap(), NOW + 1)
        .unwrap();
    assert_eq!(fresh.phase(), Phase::AwaitingPeer);
    assert_eq!(
        fresh.send("premature", TEXT, NOW + 1),
        Err(Error::WrongPhase)
    );
    let transcript = fresh.peer_challenge_transcript().unwrap();
    let response = old
        .alice_group
        .create_message(&old.alice_provider, &old.alice_signer, &transcript)
        .unwrap()
        .to_bytes()
        .unwrap();
    fresh.confirm_peer(&response, NOW + 1).unwrap();
    assert_eq!(
        fresh.group.as_ref().unwrap().epoch(),
        old.alice_group.epoch()
    );
    assert!(
        !fresh
            .group
            .as_ref()
            .unwrap()
            .members()
            .any(|m| m.credential.serialized_content() == b"bob")
    );
    let wire = fresh.send("fresh-message", TEXT, NOW + 1).unwrap();
    let message = MlsMessageIn::tls_deserialize_exact(&wire)
        .unwrap()
        .try_into_protocol_message()
        .unwrap();
    let processed = old
        .alice_group
        .process_message(&old.alice_provider, message)
        .unwrap();
    let ProcessedMessageContent::ApplicationMessage(app) = processed.into_content() else {
        panic!("fixture application");
    };
    assert_eq!(app.into_bytes(), TEXT);
    assert_eq!(
        restored.send("rollback-send", TEXT, NOW + 1),
        Err(Error::Restored)
    );
    assert_eq!(restored.pending("old-message").unwrap(), Some(original));
}
#[test]
fn actual_authorized_merge_failure_rolls_back_epoch_outbox_and_stage_deletion() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.live();
    let wire = f.self_update();
    let handle = f.core.stage_commit(&wire, NOW).unwrap();
    f.core.inspect_stage(handle).unwrap();
    let authorized = f.core.authorize_stage(handle, NOW).unwrap();
    let epoch = f.core.group.as_ref().unwrap().epoch();
    f.core.connection.execute_batch("CREATE TRIGGER fail_merge BEFORE DELETE ON core_stage BEGIN SELECT RAISE(ABORT,'fixture');END;").unwrap();
    assert_eq!(
        f.core.merge_authorized(authorized, "merge", NOW),
        Err(Error::Database)
    );
    let provider = Provider::new(&f.core.connection, &f.core.crypto);
    assert_eq!(
        MlsGroup::load(provider.storage(), &GroupId::from_slice(GROUP))
            .unwrap()
            .unwrap()
            .epoch(),
        epoch
    );
    assert_eq!(count(&f.core.connection, "core_stage"), 1);
    assert_eq!(count(&f.core.connection, "core_outbox"), 0);
    assert_eq!(f.core.phase(), Phase::Quarantined);
}
#[test]
fn actual_stage_record_failure_rolls_back_receive_ratchet() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.live();
    let wire = f.self_update();
    let before = secret(&f.core.connection);
    f.core.connection.execute_batch("CREATE TRIGGER fail_stage BEFORE INSERT ON core_stage BEGIN SELECT RAISE(ABORT,'fixture');END;").unwrap();
    assert_eq!(f.core.stage_commit(&wire, NOW), Err(Error::Database));
    assert_eq!(secret(&f.core.connection), before);
    assert_eq!(count(&f.core.connection, "core_stage"), 0);
    assert_eq!(f.core.phase(), Phase::Quarantined);
}
#[test]
fn successful_authorization_durably_advances_clock_and_backward_merge_fails() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.live();
    let wire = f.self_update();
    let handle = f.core.stage_commit(&wire, NOW).unwrap();
    f.core.inspect_stage(handle).unwrap();
    let auth = f.core.authorize_stage(handle, NOW + 10).unwrap();
    assert_eq!(stored_floor(&f.core.connection).unwrap(), (1, NOW + 10));
    assert_eq!(
        f.core.merge_authorized(auth, "clock", NOW + 9),
        Err(Error::Trust)
    );
    assert_eq!(count(&f.core.connection, "core_stage"), 1);
    assert_eq!(f.core.phase(), Phase::Quarantined);
}
#[test]
fn approved_removal_of_this_device_commits_but_permanently_blocks_sender() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.live();
    let signed = roster(
        2,
        vec![("alice", "desktop", b"alice", f.alice_signer.public())],
    );
    f.core.install_roster(&signed, NOW + 1).unwrap();
    let index = f
        .alice_group
        .members()
        .find(|m| m.credential.serialized_content() == b"bob")
        .unwrap()
        .index;
    let (commit, _, _) = f
        .alice_group
        .remove_members(&f.alice_provider, &f.alice_signer, &[index])
        .unwrap();
    f.alice_group
        .merge_pending_commit(&f.alice_provider)
        .unwrap();
    let handle = f
        .core
        .stage_commit(&commit.to_bytes().unwrap(), NOW + 1)
        .unwrap();
    assert_eq!(
        f.core.inspect_stage(handle).unwrap().removals,
        vec![index.u32()]
    );
    let auth = f.core.authorize_stage(handle, NOW + 1).unwrap();
    f.core.merge_authorized(auth, "removed", NOW + 1).unwrap();
    assert_eq!(f.core.phase(), Phase::Quarantined);
    assert_eq!(
        f.core.send("forbidden", TEXT, NOW + 1),
        Err(Error::Quarantined)
    );
    assert_eq!(count(&f.core.connection, "core_stage"), 0);
    assert_eq!(count(&f.core.connection, "core_outbox"), 1);
}
#[test]
fn expired_signed_roster_and_revision_overflow_never_encrypt_for_publication() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("expiry.sqlite"));
    f.live();
    let before = secret(&f.core.connection);
    assert_eq!(f.core.send("expired", TEXT, NOW + 1000), Err(Error::Trust));
    assert_eq!(secret(&f.core.connection), before);
    assert_eq!(f.core.phase(), Phase::Quarantined);
    let mut other = Fixture::offered(&dir.path().join("overflow.sqlite"));
    other.live();
    other
        .core
        .connection
        .execute("UPDATE core_state SET revision=?", [i64::MAX])
        .unwrap();
    other.core.revision = i64::MAX;
    let before = secret(&other.core.connection);
    assert_eq!(other.core.send("overflow", TEXT, NOW), Err(Error::Limit));
    assert_eq!(secret(&other.core.connection), before);
    assert_eq!(count(&other.core.connection, "core_outbox"), 0);
    assert_eq!(other.core.phase(), Phase::Quarantined);
}
#[test]
fn authorization_database_failure_drops_live_capabilities() {
    let dir = run_dir();
    let mut f = Fixture::offered(&dir.path().join("state.sqlite"));
    f.live();
    let wire = f.self_update();
    let handle = f.core.stage_commit(&wire, NOW).unwrap();
    f.core.inspect_stage(handle).unwrap();
    f.core.connection.execute_batch("CREATE TRIGGER fail_auth BEFORE UPDATE OF observed_time ON core_state BEGIN SELECT RAISE(ABORT,'fixture');END;").unwrap();
    assert!(matches!(
        f.core.authorize_stage(handle, NOW + 1),
        Err(Error::Database)
    ));
    assert_eq!(f.core.phase(), Phase::Quarantined);
    assert_eq!(
        f.core.send("forbidden", TEXT, NOW + 1),
        Err(Error::Quarantined)
    );
    assert_eq!(count(&f.core.connection, "core_stage"), 1);
}

#[test]
fn one_decrypt_dispatch_interleaved_source_then_chat_preserves_actual_private_ratchet() {
    let dir = run_dir();
    let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("dispatch.sqlite"));
    let (epoch, generation, _, _, _) = sdk.current(NOW).unwrap();
    let sender = peer.alice_group.own_leaf_index().u32();
    let mut source = peer_envelope("MnemaTalk ProtectedSource", EVENT);
    let Value::Array(fields) = &mut source else {
        panic!("fixture")
    };
    fields[9] = Value::Array(vec![
        Value::Integer(epoch.into()),
        Value::Integer(sender.into()),
        Value::Integer(42.into()),
        Value::Text(SOURCE.into()),
        Value::Text("opus".into()),
    ]);
    let wire = peer.peer_message(&encode(&source));
    let received = sdk.receive_protected(EVENT, &wire, NOW).unwrap();
    let ProtectedReceived::Source(received) = received else {
        panic!("wrong dispatcher")
    };
    let facts = received.facts();
    assert_eq!(facts.account(), "alice");
    assert_eq!(facts.device(), "desktop");
    assert_eq!(facts.source_id(), SOURCE);
    assert_eq!(facts.event_id(), EVENT);
    assert_eq!(facts.channel(), CHANNEL);
    assert_eq!(facts.group(), GROUP);
    assert_eq!(facts.epoch(), epoch);
    assert_eq!(facts.roster_generation(), generation);
    assert_eq!(facts.sender_index(), sender);
    assert_eq!(facts.context(), 42);
    assert_eq!(facts.codec(), SourceKind::Opus);
    let chat_event = "77777777-7777-4777-8777-777777777777";
    let chat = peer.peer_message(&encode(&peer_envelope(
        "MnemaTalk ProtectedChat",
        chat_event,
    )));
    let ProtectedReceived::Chat(chat) = sdk.receive_protected(chat_event, &chat, NOW).unwrap()
    else {
        panic!("wrong dispatcher")
    };
    assert_eq!(chat.body, "authenticated peer chat");
    assert!(sdk.matches_native_actor("profile", "main", "session"));
    assert!(sdk.receive_protected(EVENT, &wire, NOW).is_err());
    assert!(!sdk.matches_native_actor("profile", "main", "session"));
}
#[test]
fn one_decrypt_unknown_domain_and_spoofed_source_author_never_publish_facts() {
    for spoofed in [false, true] {
        let dir = run_dir();
        let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("dispatch-bad.sqlite"));
        let mut value = peer_envelope(
            if spoofed {
                "MnemaTalk ProtectedSource"
            } else {
                "Unknown ProtectedDomain"
            },
            EVENT,
        );
        if spoofed {
            let Value::Array(fields) = &mut value else {
                panic!("fixture")
            };
            fields[6] = Value::Text("bob".into());
        }
        let wire = peer.peer_message(&encode(&value));
        assert!(sdk.receive_protected(EVENT, &wire, NOW).is_err());
        assert!(!sdk.matches_native_actor("profile", "main", "session"));
        assert!(sdk.receive_chat(EVENT, &wire, NOW).is_err()); // no second decrypt fallback
    }
}

#[test]
fn one_decrypt_bound_voice_source_receipt_preserves_all_transport_claims_then_chat() {
    let dir = run_dir();
    let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("bound-dispatch.sqlite"));
    let (epoch, _, _, _, _) = sdk.current(NOW).unwrap();
    let sender = peer.alice_group.own_leaf_index().u32();
    let claim = SourceTransportClaim::claim(
        SourceKind::Opus,
        CHANNEL,
        "88888888-8888-4888-8888-888888888888",
        "99999999-9999-4999-8999-999999999999",
        SourcePurpose::Voice,
        7,
        "actual-peer-track",
    )
    .unwrap();
    let mut payload = vec![
        Value::Integer(epoch.into()),
        Value::Integer(sender.into()),
        Value::Integer(12.into()),
        Value::Text(SOURCE.into()),
        Value::Text("opus".into()),
    ];
    payload.extend(claim.payload_tail());
    let mut message = peer_envelope("MnemaTalk ProtectedVoiceSource", EVENT);
    let Value::Array(fields) = &mut message else {
        panic!("fixture")
    };
    fields[9] = Value::Array(payload);
    let wire = peer.peer_message(&encode(&message));
    let ProtectedReceived::Source(received) = sdk.receive_protected(EVENT, &wire, NOW).unwrap()
    else {
        panic!("wrong dispatch")
    };
    let facts = received.facts();
    let bound = facts.voice_binding().unwrap();
    assert_eq!(facts.account(), "alice");
    assert_eq!(facts.device(), "desktop");
    assert_eq!(bound.voice_channel(), CHANNEL);
    assert_eq!(bound.room_incarnation(), claim.room_incarnation());
    assert_eq!(bound.publisher_connection(), claim.publisher_connection());
    assert_eq!(bound.publisher_track_id(), "actual-peer-track");
    assert_eq!(bound.capture_generation(), 7);
    assert!(bound.purpose() == SourcePurpose::Voice);
    // A different native room/connection/track cannot match these authenticated
    // facts; no selected routing hint replaces the signed inner claim.
    assert_ne!(
        bound.room_incarnation(),
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
    );
    assert_ne!(
        bound.publisher_connection(),
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
    );
    assert_ne!(bound.publisher_track_id(), "different-track");
    let event = "77777777-7777-4777-8777-777777777777";
    let wire = peer.peer_message(&encode(&peer_envelope("MnemaTalk ProtectedChat", event)));
    assert!(matches!(
        sdk.receive_protected(event, &wire, NOW),
        Ok(ProtectedReceived::Chat(_))
    ));
    assert!(sdk.matches_native_actor("profile", "main", "session"));
}
#[test]
fn no_matching_voice_channel_reservation_has_no_context_ratchet_or_outbox_mutation() {
    let dir = run_dir();
    let (mut sdk, _) = sdk_fixture(&dir.path().join("no-voice.sqlite"));
    let claim = SourceTransportClaim::claim(
        SourceKind::Opus,
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
        "88888888-8888-4888-8888-888888888888",
        "99999999-9999-4999-8999-999999999999",
        SourcePurpose::Voice,
        7,
        "track",
    )
    .unwrap();
    let before = secret(&sdk.core.connection);
    let contexts = count(&sdk.core.connection, "sdk_sources");
    let outbox = count(&sdk.core.connection, "core_outbox");
    assert!(matches!(
        sdk.reserve_bound_voice_source(EVENT, SOURCE, &claim, NOW),
        Err(Error::Unauthorized)
    ));
    assert_eq!(secret(&sdk.core.connection), before);
    assert_eq!(count(&sdk.core.connection, "sdk_sources"), contexts);
    assert_eq!(count(&sdk.core.connection, "core_outbox"), outbox);
    assert!(sdk.matches_native_actor("profile", "main", "session"));
}
#[test]
fn authenticated_voice_source_wrong_channel_or_invalid_purpose_never_returns_binding() {
    for wrong_channel in [true, false] {
        let dir = run_dir();
        let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("voice-bad.sqlite"));
        let (epoch, _, _, _, _) = sdk.current(NOW).unwrap();
        let sender = peer.alice_group.own_leaf_index().u32();
        let claim = SourceTransportClaim::claim(
            SourceKind::Opus,
            CHANNEL,
            "88888888-8888-4888-8888-888888888888",
            "99999999-9999-4999-8999-999999999999",
            SourcePurpose::Voice,
            7,
            "track",
        )
        .unwrap();
        let mut tail = claim.payload_tail();
        tail[if wrong_channel { 0 } else { 3 }] = Value::Text(
            if wrong_channel {
                "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
            } else {
                "camera"
            }
            .into(),
        );
        let mut payload = vec![
            Value::Integer(epoch.into()),
            Value::Integer(sender.into()),
            Value::Integer(12.into()),
            Value::Text(SOURCE.into()),
            Value::Text("opus".into()),
        ];
        payload.extend(tail);
        let mut value = peer_envelope("MnemaTalk ProtectedVoiceSource", EVENT);
        let Value::Array(fields) = &mut value else {
            panic!("fixture")
        };
        fields[9] = Value::Array(payload);
        let wire = peer.peer_message(&encode(&value));
        assert!(sdk.receive_protected(EVENT, &wire, NOW).is_err());
        assert!(!sdk.matches_native_actor("profile", "main", "session"));
    }
}

#[test]
fn typed_chat_events_actual_private_mls_dispatch_all_actions_without_text_commands() {
    let dir = run_dir();
    let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("typed-events.sqlite"));
    let original = EVENT.to_owned();
    let edit = "44444444-4444-4444-8444-444444444444";
    let events = [
        (
            EVENT,
            ChatOperation::Create {
                message_id: original.clone(),
                parent_id: None,
                body: "/delete is ordinary text 😀".into(),
            },
        ),
        (
            edit,
            ChatOperation::Edit {
                message_id: original.clone(),
                expected_revision: original.clone(),
                body: "actual encrypted edit".into(),
            },
        ),
        (
            "55555555-5555-4555-8555-555555555555",
            ChatOperation::Delete {
                message_id: original.clone(),
                expected_revision: edit.into(),
            },
        ),
        (
            "66666666-6666-4666-8666-666666666666",
            ChatOperation::Reply {
                message_id: "66666666-6666-4666-8666-666666666666".into(),
                parent_id: None,
                reply_to_id: original.clone(),
                body: "actual encrypted quote".into(),
            },
        ),
        (
            "77777777-7777-4777-8777-777777777777",
            ChatOperation::Reaction {
                message_id: original.clone(),
                emoji: "👍".into(),
                action: ReactionAction::Add,
            },
        ),
        (
            "88888888-8888-4888-8888-888888888888",
            ChatOperation::Reaction {
                message_id: original.clone(),
                emoji: "👍".into(),
                action: ReactionAction::Remove,
            },
        ),
    ];
    for (event, operation) in events {
        let kind = operation.kind();
        let claim = ChatEventClaim::claim(operation).unwrap();
        let mut envelope = peer_envelope("MnemaTalk ProtectedChatEvent", event);
        let Value::Array(fields) = &mut envelope else {
            panic!("fixture envelope")
        };
        fields[9] = claim.payload();
        let wire = peer.peer_message(&encode(&envelope));
        let ProtectedReceived::ChatEvent(received) =
            sdk.receive_protected(event, &wire, NOW).unwrap()
        else {
            panic!("wrong domain")
        };
        assert_eq!(received.event_id(), event);
        assert_eq!(received.account(), "alice");
        assert_eq!(received.device(), "desktop");
        assert_eq!(received.operation().kind(), kind);
        // Typed authentication is not history/role authorization: this fixture
        // verifies all operations including delete-before-reply, but never
        // applies any event to a display/history or claims mutation permission.
    }
    assert_eq!(count(&sdk.core.connection, "core_chat_archive"), 6);
    let event = "99999999-9999-4999-8999-999999999999";
    let wire = peer.peer_message(&encode(&peer_envelope("MnemaTalk ProtectedChat", event)));
    let ProtectedReceived::Chat(received) = sdk.receive_protected(event, &wire, NOW).unwrap()
    else {
        panic!("wrong legacy domain")
    };
    assert_eq!(received.body, "authenticated peer chat");
    assert_eq!(count(&sdk.core.connection, "core_chat_archive"), 6);
}

#[test]
fn typed_chat_actual_durable_outbox_and_suppressed_storage_never_return_ciphertext() {
    for suppressed in [
        None,
        Some("core_outbox"),
        Some("sdk_events"),
        Some("core_chat_archive"),
    ] {
        let dir = run_dir();
        let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("typed-send.sqlite"));
        let claim = ChatEventClaim::claim(ChatOperation::Create {
            message_id: EVENT.into(),
            parent_id: None,
            body: "typed durable fixture".into(),
        })
        .unwrap();
        let before = secret(&sdk.core.connection);
        if let Some(table) = suppressed {
            sdk.core.connection.execute_batch(&format!("CREATE TRIGGER suppress BEFORE INSERT ON {table} BEGIN SELECT RAISE(IGNORE);END;")).unwrap();
            assert_eq!(
                sdk.send_chat_event(EVENT, &claim, NOW),
                Err(Error::Database)
            );
            assert_eq!(secret(&sdk.core.connection), before);
            assert_eq!(count(&sdk.core.connection, "core_outbox"), 0);
            assert_eq!(count(&sdk.core.connection, "core_chat_archive"), 0);
            assert!(!sdk.matches_native_actor("profile", "main", "session"));
        } else {
            let wire = sdk.send_chat_event(EVENT, &claim, NOW).unwrap();
            assert_eq!(
                sdk.pending_chat_for_native_publish(EVENT, NOW).unwrap(),
                Some(wire.clone())
            );
            let value: Value =
                coset::cbor::de::from_reader(peer.decrypt(&wire).as_slice()).unwrap();
            let Value::Array(fields) = value else {
                panic!("fixture envelope")
            };
            assert_eq!(
                fields[0],
                Value::Text("MnemaTalk ProtectedChatEvent".into())
            );
            assert_eq!(fields[6], Value::Text("bob".into()));
            assert_eq!(fields[7], Value::Text("desktop".into()));
            assert_eq!(fields[8], Value::Text(EVENT.into()));
            assert_eq!(fields[9], claim.payload());
            assert_ne!(secret(&sdk.core.connection), before);
            assert_eq!(count(&sdk.core.connection, "core_chat_archive"), 1);
        }
    }
}

#[test]
fn typed_chat_authenticated_bad_author_version_event_or_payload_retires_without_fallback() {
    for fault in 0..5 {
        let dir = run_dir();
        let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("typed-bad.sqlite"));
        let claim = ChatEventClaim::claim(ChatOperation::Create {
            message_id: EVENT.into(),
            parent_id: None,
            body: "typed malformed fixture".into(),
        })
        .unwrap();
        let mut envelope = peer_envelope("MnemaTalk ProtectedChatEvent", EVENT);
        let Value::Array(fields) = &mut envelope else {
            panic!("fixture envelope")
        };
        fields[9] = claim.payload();
        match fault {
            0 => fields[6] = Value::Text("bob".into()),
            1 => fields[7] = Value::Text("unknown-device".into()),
            2 => {
                let Value::Array(payload) = &mut fields[9] else {
                    panic!("fixture payload")
                };
                payload[0] = Value::Integer(2.into());
            }
            3 => {
                let Value::Array(payload) = &mut fields[9] else {
                    panic!("fixture payload")
                };
                payload[2] = Value::Text(SOURCE.into());
            }
            _ => fields[9] = Value::Text("/delete ordinary text cannot be typed command".into()),
        }
        let wire = peer.peer_message(&encode(&envelope));
        let before = secret(&sdk.core.connection);
        let revision = sdk.core.revision;
        assert!(sdk.receive_protected(EVENT, &wire, NOW).is_err());
        assert_eq!(secret(&sdk.core.connection), before);
        assert_eq!(sdk.core.revision, revision);
        assert_eq!(count(&sdk.core.connection, "core_chat_archive"), 0);
        assert!(!sdk.matches_native_actor("profile", "main", "session"));
        assert!(sdk.receive_chat(EVENT, &wire, NOW).is_err());
    }
}

#[test]
fn typed_chat_receive_archive_failure_rolls_back_and_retires_actual_receiver() {
    for trigger in [
        "SELECT RAISE(IGNORE)",
        "SELECT RAISE(ABORT,'fixture failure')",
    ] {
        let dir = run_dir();
        let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("archive-failure.sqlite"));
        sdk.core.connection.execute_batch(&format!(
            "CREATE TRIGGER suppress_archive BEFORE INSERT ON core_chat_archive BEGIN {trigger}; END;"
        )).unwrap();
        let claim = projection_claim();
        let mut envelope = peer_envelope(CHAT_EVENT_DOMAIN, EVENT);
        let Value::Array(fields) = &mut envelope else {
            panic!("fixture envelope")
        };
        fields[9] = claim.payload();
        let wire = peer.peer_message(&encode(&envelope));
        let before = secret(&sdk.core.connection);
        let revision = sdk.core.revision;
        assert!(matches!(
            sdk.receive_protected(EVENT, &wire, NOW),
            Err(Error::Database)
        ));
        assert_eq!(secret(&sdk.core.connection), before);
        assert_eq!(sdk.core.revision, revision);
        assert_eq!(count(&sdk.core.connection, "core_chat_archive"), 0);
        assert!(!sdk.matches_native_actor("profile", "main", "session"));
        assert!(sdk.receive_protected(EVENT, &wire, NOW).is_err());
    }
}

#[test]
fn typed_chat_archive_survives_sqlcipher_reopen_without_restoring_send_authority() {
    let dir = run_dir();
    let path = dir.path().join("archive-reopen.sqlite");
    let (mut sdk, mut peer) = sdk_fixture(&path);
    let peer_key = peer.alice_signer.public().to_vec();
    let claim = projection_claim();
    let mut envelope = peer_envelope(CHAT_EVENT_DOMAIN, EVENT);
    let Value::Array(fields) = &mut envelope else {
        panic!("fixture envelope")
    };
    fields[9] = claim.payload();
    let plaintext = encode(&envelope);
    let wire = peer.peer_message(&plaintext);
    let ProtectedReceived::ChatEvent(received) = sdk.receive_protected(EVENT, &wire, NOW).unwrap()
    else {
        panic!("wrong domain")
    };
    let expected_sender = peer.alice_group.own_leaf_index().u32();
    let expected_epoch = received.scope().epoch().to_be_bytes();
    let expected_generation = received.scope().root_generation().to_be_bytes();
    let own_event = "99999999-9999-4999-8999-999999999999";
    let own_claim = ChatEventClaim::claim(ChatOperation::Create {
        message_id: own_event.into(),
        parent_id: None,
        body: "own persistent encrypted history".into(),
    })
    .unwrap();
    let own_wire = sdk.send_chat_event(own_event, &own_claim, NOW).unwrap();
    drop(sdk);
    drop(peer);
    let mut restored = Core::restore_inspection_only(&path, &FixtureKeys, pin(&peer_key)).unwrap();
    let row: (String, String, String, u32, Vec<u8>, Vec<u8>, Vec<u8>, Vec<u8>) = restored.connection.query_row(
        "SELECT channel,account,device,sender,epoch,generation,wire_hash,plaintext FROM core_chat_archive WHERE event=?",
        [EVENT], |r| Ok((r.get(0)?,r.get(1)?,r.get(2)?,r.get(3)?,r.get(4)?,r.get(5)?,r.get(6)?,r.get(7)?))
    ).unwrap();
    assert_eq!(
        row,
        (
            CHANNEL.into(),
            "alice".into(),
            "desktop".into(),
            expected_sender,
            expected_epoch.into(),
            expected_generation.into(),
            sha2::Sha256::digest(&wire).to_vec(),
            plaintext
        )
    );
    assert_eq!(count(&restored.connection, "core_chat_archive"), 2);
    assert_eq!(restored.pending(own_event).unwrap(), Some(own_wire));
    assert_eq!(restored.phase(), Phase::Restored);
    assert_eq!(restored.send("denied", TEXT, NOW), Err(Error::Restored));
    assert_eq!(
        restored.receive_application(&wire, NOW),
        Err(Error::Restored)
    );
    assert!(
        !std::fs::read(&path)
            .unwrap()
            .windows(b"native exact reserved fixture".len())
            .any(|part| part == b"native exact reserved fixture")
    );
}

#[test]
fn typed_chat_archive_budgets_reject_send_and_receive_without_consuming_state() {
    for (rows, size) in [(512, 32768), (32768, 1)] {
        for own_send in [false, true] {
            let dir = run_dir();
            let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("archive-budget.sqlite"));
            // Synthetic rows exercise real encrypted storage bounds only;
            // they are never treated as authenticated historical facts.
            sdk.core.connection.execute_batch(&format!(
                "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<{rows})
                 INSERT INTO core_chat_archive SELECT printf('%08x-0000-4000-8000-000000000000',i),
                 '11111111-1111-4111-8111-111111111111',zeroblob(8),zeroblob(8),'fixture','fixture',0,
                 zeroblob(32),zeroblob({size}) FROM n;"
            )).unwrap();
            let claim = projection_claim();
            let mut envelope = peer_envelope(CHAT_EVENT_DOMAIN, EVENT);
            let Value::Array(fields) = &mut envelope else {
                panic!("fixture envelope")
            };
            fields[9] = claim.payload();
            let wire = peer.peer_message(&encode(&envelope));
            let before = secret(&sdk.core.connection);
            let revision = sdk.core.revision;
            if own_send {
                assert_eq!(sdk.send_chat_event(EVENT, &claim, NOW), Err(Error::Limit));
            } else {
                assert!(matches!(
                    sdk.receive_protected(EVENT, &wire, NOW),
                    Err(Error::Limit)
                ));
            }
            assert_eq!(secret(&sdk.core.connection), before);
            assert_eq!(sdk.core.revision, revision);
            assert_eq!(count(&sdk.core.connection, "core_chat_archive"), rows);
            assert_eq!(count(&sdk.core.connection, "core_outbox"), 0);
            assert_eq!(count(&sdk.core.connection, "sdk_events"), 0);
            assert!(!sdk.matches_native_actor("profile", "main", "session"));
        }
    }
}

fn projection_claim() -> ChatEventClaim {
    ChatEventClaim::claim(ChatOperation::Create {
        message_id: EVENT.into(),
        parent_id: None,
        body: "native exact reserved fixture".into(),
    })
    .unwrap()
}

#[test]
fn native_projection_actual_durable_reservation_repeated_projection_never_changes_ratchet_or_wire()
{
    let dir = run_dir();
    let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("projection.sqlite"));
    let before = secret(&sdk.core.connection);
    let reserved = sdk
        .reserve_native_chat_event(EVENT, &projection_claim(), NOW)
        .unwrap();
    let after = secret(&sdk.core.connection);
    assert_ne!(before, after);
    let wire = reserved.wire_for_native_relay().to_vec();
    let rows = count(&sdk.core.connection, "core_outbox");
    let scope = sdk.native_protected_event_scope(NOW).unwrap();
    for _ in 0..3 {
        let projected = sdk
            .pending_native_chat_event_projection(&reserved, NOW)
            .unwrap();
        assert_eq!(projected.event_id(), EVENT);
        assert_eq!(projected.account(), "bob");
        assert_eq!(projected.device(), "desktop");
        assert!(projected.scope().matches_exact(&scope));
        assert_eq!(secret(&sdk.core.connection), after);
        assert_eq!(count(&sdk.core.connection, "core_outbox"), rows);
        assert_eq!(
            sdk.pending_chat_for_native_publish(EVENT, NOW)
                .unwrap()
                .unwrap(),
            wire
        );
    }
    // Only the real opposite peer consumes this private wire. Own projection
    // has never invoked MLS decrypt and remains compatible with the next peer send.
    let value: Value = coset::cbor::de::from_reader(peer.decrypt(&wire).as_slice()).unwrap();
    let Value::Array(fields) = value else {
        panic!("fixture envelope")
    };
    assert_eq!(fields[9], projection_claim().payload());
    let incoming = "44444444-4444-4444-8444-444444444444";
    let claim = ChatEventClaim::claim(ChatOperation::Create {
        message_id: incoming.into(),
        parent_id: None,
        body: "actual peer after own projection".into(),
    })
    .unwrap();
    let mut envelope = peer_envelope(CHAT_EVENT_DOMAIN, incoming);
    let Value::Array(fields) = &mut envelope else {
        panic!("fixture envelope")
    };
    fields[9] = claim.payload();
    let received_wire = peer.peer_message(&encode(&envelope));
    let ProtectedReceived::ChatEvent(received) = sdk
        .receive_protected(incoming, &received_wire, NOW)
        .unwrap()
    else {
        panic!("fixture domain")
    };
    assert_eq!(received.account(), "alice");
    assert!(received.scope().matches_exact(&scope));
    assert!(
        sdk.pending_native_chat_event_projection(&reserved, NOW)
            .is_ok()
    );
}

#[test]
fn native_projection_actual_foreign_owner_and_changed_durable_wire_are_denied() {
    let dir = run_dir();
    let (mut first, _) = sdk_fixture(&dir.path().join("first.sqlite"));
    let (second, _) = sdk_fixture(&dir.path().join("second.sqlite"));
    let reserved = first
        .reserve_native_chat_event(EVENT, &projection_claim(), NOW)
        .unwrap();
    let first_scope = first.native_protected_event_scope(NOW).unwrap();
    let second_scope = second.native_protected_event_scope(NOW).unwrap();
    // Same external labels, group bytes, epoch and fixture credential still
    // cannot substitute another actual provider owner.
    assert!(!first_scope.same_native_context(&second_scope));
    let before = secret(&second.core.connection);
    assert!(matches!(
        second.pending_native_chat_event_projection(&reserved, NOW),
        Err(Error::Stale)
    ));
    assert_eq!(secret(&second.core.connection), before);
    first
        .core
        .connection
        .execute(
            "UPDATE core_outbox SET wire=? WHERE event_id=?",
            params![b"altered local ciphertext".as_slice(), EVENT],
        )
        .unwrap();
    assert!(matches!(
        first.pending_native_chat_event_projection(&reserved, NOW),
        Err(Error::Stale)
    ));
}

#[test]
fn native_projection_actual_epoch_roster_removed_leaf_and_retirement_never_borrow_new_authority() {
    for change in 0..4 {
        let dir = run_dir();
        let (mut sdk, mut peer) = sdk_fixture(&dir.path().join("stale.sqlite"));
        let original = sdk.native_protected_event_scope(NOW).unwrap();
        let reserved = sdk
            .reserve_native_chat_event(EVENT, &projection_claim(), NOW)
            .unwrap();
        match change {
            0 => {
                let commit = peer.self_update();
                let staged = sdk.stage_native_commit(&commit, NOW).unwrap();
                sdk.inspect_native_commit(staged).unwrap();
                let approval = sdk.authorize_native_commit(staged, NOW).unwrap();
                sdk.merge_native_commit(approval, "44444444-4444-4444-8444-444444444444", NOW)
                    .unwrap();
                let current = sdk.native_protected_event_scope(NOW).unwrap();
                assert!(original.same_native_context(&current));
                assert!(!original.matches_exact(&current));
            }
            1 => {
                sdk.install_native_roster(&roster(2, peer.entries()), NOW)
                    .unwrap();
                let current = sdk.native_protected_event_scope(NOW).unwrap();
                assert!(original.same_native_context(&current));
                assert!(!original.matches_exact(&current));
            }
            2 => {
                let revoked = roster(
                    2,
                    vec![("alice", "desktop", b"alice", peer.alice_signer.public())],
                );
                sdk.install_native_roster(&revoked, NOW).unwrap();
            }
            _ => sdk.retire_native(),
        }
        assert!(
            sdk.pending_native_chat_event_projection(&reserved, NOW)
                .is_err()
        );
    }
}

#[test]
fn native_projection_actual_repeated_reserve_is_replay_and_suppressed_outbox_never_returns_handle()
{
    for suppress in [false, true] {
        let dir = run_dir();
        let (mut sdk, _) = sdk_fixture(&dir.path().join("reserve.sqlite"));
        let before = secret(&sdk.core.connection);
        if suppress {
            sdk.core.connection.execute_batch("CREATE TRIGGER suppress BEFORE INSERT ON core_outbox BEGIN SELECT RAISE(IGNORE);END;").unwrap();
            assert!(matches!(
                sdk.reserve_native_chat_event(EVENT, &projection_claim(), NOW),
                Err(Error::Database)
            ));
            assert_eq!(secret(&sdk.core.connection), before);
            assert_eq!(count(&sdk.core.connection, "core_outbox"), 0);
            assert!(!sdk.matches_native_actor("profile", "main", "session"));
        } else {
            let reserved = sdk
                .reserve_native_chat_event(EVENT, &projection_claim(), NOW)
                .unwrap();
            let after = secret(&sdk.core.connection);
            let rows = count(&sdk.core.connection, "core_outbox");
            assert!(matches!(
                sdk.reserve_native_chat_event(EVENT, &projection_claim(), NOW),
                Err(Error::Replay)
            ));
            assert_eq!(secret(&sdk.core.connection), after);
            assert_eq!(count(&sdk.core.connection, "core_outbox"), rows);
            assert_eq!(reserved.event_id(), EVENT);
            assert!(!sdk.matches_native_actor("profile", "main", "session"));
        }
    }
}
