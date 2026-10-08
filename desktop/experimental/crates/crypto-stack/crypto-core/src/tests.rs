use super::*;
use coset::{CoseSign1Builder, HeaderBuilder, iana};
use ed25519_dalek::{Signer, SigningKey};
use openmls_rust_crypto::OpenMlsRustCrypto;
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
