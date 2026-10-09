use super::*;
use mnema_crypto_enrollment_prototype::{Device, NativeAdmissionIntent, NativeSecrets};
use mnema_private_first_community_bootstrap::{FreshCommunity, NativeAdminSubject};
use openmls::prelude::tls_codec::Serialize as _;
use std::{collections::HashMap, sync::Mutex};
const NOW: u64 = 1000;
const CHANNEL: &str = "11111111-1111-4111-8111-111111111111";
const COMMIT: &str = "22222222-2222-4222-8222-222222222222";
const WELCOME: &str = "33333333-3333-4333-8333-333333333333";
const CONFIRM: &str = "44444444-4444-4444-8444-444444444444";
const CHAT: &str = "55555555-5555-4555-8555-555555555555";
struct FixtureSeeds(Mutex<HashMap<String, Zeroizing<[u8; 32]>>>, Option<u8>);
impl FixtureSeeds {
    fn fresh() -> Self {
        Self(Mutex::new(HashMap::new()), None)
    }
}
impl NativeSecrets for FixtureSeeds {
    fn create_seed(&self, alias: &str) -> mnema_crypto_enrollment_prototype::Result<[u8; 32]> {
        let mut values = self.0.lock().unwrap();
        if values.contains_key(alias) {
            return Err(mnema_crypto_enrollment_prototype::Error::NativeUnavailable);
        }
        let mut bytes = Zeroizing::new([0; 32]);
        if let Some(marker) = self.1 {
            bytes.fill(
                marker
                    + match alias {
                        "issuer-root" => 1,
                        "database-key" => 2,
                        "device-key" => 3,
                        _ => 4,
                    },
            )
        } else {
            getrandom::fill(bytes.as_mut()).unwrap();
        }
        let public = ed25519_dalek::SigningKey::from_bytes(&bytes)
            .verifying_key()
            .to_bytes();
        values.insert(alias.into(), bytes);
        Ok(public)
    }
    fn read_seed(
        &self,
        alias: &str,
    ) -> mnema_crypto_enrollment_prototype::Result<Zeroizing<[u8; 32]>> {
        self.0
            .lock()
            .unwrap()
            .get(alias)
            .cloned()
            .ok_or(mnema_crypto_enrollment_prototype::Error::NativeUnavailable)
    }
}
fn scope() -> Scope {
    Scope::new("https://host.example", "fixture-community").unwrap()
}
fn subject() -> NativeAdminSubject {
    NativeAdminSubject::from_native_admin_workflow("alice", "desktop", b"alice-native").unwrap()
}
fn binding(account: &str) -> NativeBinding {
    NativeBinding::from_native_actor(CHANNEL, "profile", "main", "session", account, "desktop")
        .unwrap()
}
fn dir() -> tempfile::TempDir {
    let p = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../evidence/runs");
    std::fs::create_dir_all(&p).unwrap();
    tempfile::Builder::new()
        .prefix("host-private-")
        .tempdir_in(p)
        .unwrap()
}
#[test]
fn actual_fresh_root_host_adoption_owner_facts_and_durable_channel_no_restored_activation() {
    let d = dir();
    let keys = FixtureSeeds::fresh();
    let fresh = FreshCommunity::provision_first_group(
        &d.path().join("root.sqlite"),
        &keys,
        scope(),
        subject(),
        NOW,
    )
    .unwrap();
    let (_, owner) = fresh.into_native_host();
    let mut sdk = Sdk::from_fresh_native_host(owner, binding("alice"), NOW).unwrap();
    let facts = sdk.native_owner_facts(NOW).unwrap();
    assert_eq!(facts.account(), "alice");
    assert_eq!(facts.device(), "desktop");
    assert_eq!(facts.identity(), b"alice-native");
    assert_eq!(facts.channel(), CHANNEL);
    assert_eq!(facts.epoch(), 0);
    assert_eq!(facts.generation(), 1);
    let wire = sdk
        .send_chat(CHAT, "fixture protected first-host text", NOW)
        .unwrap();
    assert_eq!(
        sdk.pending_chat_for_native_publish(CHAT, NOW)
            .unwrap()
            .unwrap(),
        wire
    );
    let stored: String = sdk
        .core
        .connection
        .query_row(
            "SELECT channel FROM native_host_channel WHERE id=1",
            [],
            |r| r.get(0),
        )
        .unwrap();
    assert_eq!(stored, CHANNEL);
    let pin = NativeBootstrap::from_native_pin(
        scope(),
        facts.group(),
        sdk.core.pin.authority,
        b"alice-native",
        facts.signature_key().try_into().unwrap(),
    )
    .unwrap();
    struct Keys<'a>(&'a FixtureSeeds);
    impl NativeKeyProvider for Keys<'_> {
        fn database_key(&self) -> Result<Zeroizing<[u8; 32]>> {
            self.0
                .read_seed("database-key")
                .map_err(|_| Error::KeyUnavailable)
        }
    }
    let restored =
        Core::restore_inspection_only(&d.path().join("root.sqlite"), &Keys(&keys), pin).unwrap();
    assert_eq!(restored.phase(), Phase::Restored);
    assert!(Sdk::bind_native(restored, binding("alice"), NOW).is_err());
    sdk.retire_native();
    assert!(sdk.native_owner_facts(NOW).is_err());
}
#[test]
fn actual_same_native_device_pairing_host_add_welcome_fresh_peer_and_protected_chat_both_directions()
 {
    actual_pairing_and_removal(RemovalScenario::Valid);
}
#[derive(Clone, Copy)]
enum RemovalScenario {
    Valid,
    LongIdentity,
    ExpiredRoster,
    RetainedPeer,
    RelabeledControl,
    WrongControlAccount,
    WrongControlChannel,
    TrailingControl,
    ReceiverBackwardsTime,
    SuppressedReceiverControl,
    SuppressedReceiverState,
    SuppressedRetainedControl,
    SuppressedRetainedState,
    SelfTarget,
    WrongKey,
    OldRoster,
    SuppressedOutbox,
    BackwardsTime,
}
#[test]
fn real_retained_peer_decrypts_future_content_after_atomic_removal() {
    actual_pairing_and_removal(RemovalScenario::RetainedPeer);
}
#[test]
fn actual_control_rejects_relabeling_wrong_account_channel_and_noncanonical_wire() {
    for scenario in [
        RemovalScenario::RelabeledControl,
        RemovalScenario::WrongControlAccount,
        RemovalScenario::WrongControlChannel,
        RemovalScenario::TrailingControl,
    ] {
        actual_pairing_and_removal(scenario);
    }
}
#[test]
fn actual_control_rejects_time_behind_live_observation_before_durable_floor_advances() {
    actual_pairing_and_removal(RemovalScenario::ReceiverBackwardsTime);
}
#[test]
fn actual_receiving_removal_rolls_back_suppressed_journal_and_state_for_retained_and_removed_devices()
 {
    for scenario in [
        RemovalScenario::SuppressedReceiverControl,
        RemovalScenario::SuppressedReceiverState,
        RemovalScenario::SuppressedRetainedControl,
        RemovalScenario::SuppressedRetainedState,
    ] {
        actual_pairing_and_removal(scenario);
    }
}
#[test]
fn actual_receiver_process_crash_keeps_provider_roster_and_control_atomic() {
    struct ChildKeys(u8);
    impl NativeKeyProvider for ChildKeys {
        fn database_key(&self) -> Result<Zeroizing<[u8; 32]>> {
            Ok(Zeroizing::new([self.0; 32]))
        }
    }
    for retained in [false, true] {
        for boundary in ["before", "after"] {
            let d = dir();
            let test = if retained {
                "host_tests::real_retained_peer_decrypts_future_content_after_atomic_removal"
            } else {
                "host_tests::actual_same_native_device_pairing_host_add_welcome_fresh_peer_and_protected_chat_both_directions"
            };
            let status = std::process::Command::new(std::env::current_exe().unwrap())
                .args(["--exact", test, "--test-threads=1"])
                .env("MNEMA_HOST_CHILD_DIR", d.path())
                .env("MNEMA_CONTROL_RECEIVER_CRASH", boundary)
                .stdout(std::process::Stdio::null())
                .stderr(std::process::Stdio::null())
                .status()
                .unwrap();
            assert_eq!(status.code(), Some(73), "retained={retained} {boundary}");
            let path = d.path().join(if retained {
                "retained.sqlite"
            } else {
                "peer.sqlite"
            });
            let c =
                open_encrypted(&path, &ChildKeys(if retained { 222 } else { 212 }), false).unwrap();
            let committed = boundary == "after";
            let old_generation = if retained { 3 } else { 2 };
            assert_eq!(
                stored_floor(&c).unwrap().0,
                old_generation + u64::from(committed)
            );
            let rows: i64 = c
                .query_row("SELECT COUNT(*) FROM core_device_removals", [], |r| {
                    r.get(0)
                })
                .unwrap();
            assert_eq!(rows, i64::from(committed));
            let group_id: Vec<u8> = c
                .query_row("SELECT group_id FROM core_pin", [], |r| r.get(0))
                .unwrap();
            let crypto = RustCrypto::default();
            let provider = Provider::new(&c, &crypto);
            let group = MlsGroup::load(provider.storage(), &GroupId::from_slice(&group_id))
                .unwrap()
                .unwrap();
            assert_eq!(group.is_active(), retained || !committed);
            // The public epoch advances for both. Removed members must remain
            // inactive; a newer public epoch does not grant its private secrets.
            let old_epoch = if retained { 2 } else { 1 };
            assert_eq!(
                group.epoch().as_u64(),
                old_epoch + u64::from(committed)
            );
            if committed {
                let (epoch, generation, removed_self): (Vec<u8>, Vec<u8>, bool) = c
                    .query_row(
                        "SELECT epoch,generation,removed_self FROM core_device_removals",
                        [],
                        |r| Ok((r.get(0)?, r.get(1)?, r.get(2)?)),
                    )
                    .unwrap();
                assert_eq!(epoch, (old_epoch + 1).to_be_bytes());
                assert_eq!(generation, (old_generation + 1).to_be_bytes());
                assert_eq!(removed_self, !retained);
            }
        }
    }
}
#[test]
fn actual_host_removes_long_identity_and_expired_historical_roster() {
    for scenario in [
        RemovalScenario::LongIdentity,
        RemovalScenario::ExpiredRoster,
    ] {
        actual_pairing_and_removal(scenario);
    }
}
#[test]
fn actual_host_rejects_wrong_removal_target_old_roster_and_suppressed_outbox() {
    for scenario in [
        RemovalScenario::SelfTarget,
        RemovalScenario::WrongKey,
        RemovalScenario::OldRoster,
        RemovalScenario::SuppressedOutbox,
        RemovalScenario::BackwardsTime,
    ] {
        actual_pairing_and_removal(scenario);
    }
}
fn actual_pairing_and_removal(scenario: RemovalScenario) {
    let child = std::env::var("MNEMA_HOST_CHILD_DIR").ok();
    let d = if child.is_none() { Some(dir()) } else { None };
    let path = child
        .as_ref()
        .map(std::path::PathBuf::from)
        .unwrap_or_else(|| d.as_ref().unwrap().path().to_owned());
    let root_keys = if child.is_some() {
        FixtureSeeds(Mutex::new(HashMap::new()), Some(200))
    } else {
        FixtureSeeds::fresh()
    };
    let peer_keys = if child.is_some() {
        FixtureSeeds(Mutex::new(HashMap::new()), Some(210))
    } else {
        FixtureSeeds::fresh()
    };
    let fresh = FreshCommunity::provision_first_group(
        &path.join("root.sqlite"),
        &root_keys,
        scope(),
        subject(),
        NOW,
    )
    .unwrap();
    let (mut issuer, transfer) = fresh.into_native_host();
    let native_pin = issuer.pin_for_native_out_of_band_transfer();
    let peer_device = Device::provision_native(&peer_keys, native_pin.clone()).unwrap();
    peer_keys.create_seed("database-key").unwrap();
    let outcome = Sdk::from_fresh_native_host(transfer, binding("alice"), NOW);
    if std::env::var("MNEMA_HOST_FAULT")
        .as_deref()
        .is_ok_and(|f| f.starts_with("adopt-ignore-"))
    {
        assert!(matches!(outcome, Err(Error::Database)));
        return;
    }
    let mut host = outcome.unwrap();
    let own = host.native_owner_facts(NOW).unwrap();
    let pin = NativeBootstrap::from_native_pin(
        scope(),
        own.group(),
        native_pin.authority_key(),
        own.identity(),
        own.signature_key().try_into().unwrap(),
    )
    .unwrap();
    let peer_identity = if matches!(scenario, RemovalScenario::LongIdentity) {
        vec![b'b'; 256]
    } else {
        b"bob-native".to_vec()
    };
    let (mut peer, offer) =
        Core::begin_native_enrollment(&path.join("peer.sqlite"), &peer_keys, pin, &peer_identity)
            .unwrap();
    assert_eq!(offer.signature_key, peer_device.public_key());
    let invitation = issuer
        .invite_native_reviewed_device(
            NativeAdmissionIntent::from_native_out_of_band_pin(
                "bob",
                "desktop",
                &offer.identity,
                offer.signature_key.clone().try_into().unwrap(),
            )
            .unwrap(),
            NOW,
        )
        .unwrap();
    let roster = issuer
        .admit_proven_device(&peer_device.respond(&invitation, NOW).unwrap(), NOW)
        .unwrap();
    host.install_native_roster(&roster, NOW).unwrap();
    peer.install_roster(&roster, NOW).unwrap();
    let result = host.add_root_approved_native_peer(
        COMMIT,
        WELCOME,
        &offer.key_package.tls_serialize_detached().unwrap(),
        NOW,
    );
    if std::env::var("MNEMA_HOST_FAULT")
        .as_deref()
        .is_ok_and(|f| f.starts_with("add-ignore-"))
    {
        assert!(matches!(result, Err(Error::Database | Error::Provider)));
        assert_eq!(host.core.phase(), Phase::Quarantined);
        return;
    }
    let admitted = result.unwrap();
    assert_eq!(
        host.pending_native_host_publication(COMMIT, NOW)
            .unwrap()
            .unwrap(),
        admitted.commit
    );
    assert_eq!(
        host.pending_native_host_publication(WELCOME, NOW)
            .unwrap()
            .unwrap(),
        admitted.welcome
    );
    peer.accept_welcome(&admitted.welcome, NOW).unwrap();
    assert_eq!(peer.phase(), Phase::AwaitingPeer);
    let transcript = peer.peer_challenge_transcript().unwrap();
    // Real two-member preflight regressions: no generic plaintext oracle and
    // no untrusted parse can consume the root sender's ratchet/outbox.
    assert!(matches!(
        host.answer_native_fresh_join(CONFIRM, b"outsider", &offer.signature_key, &transcript, NOW),
        Err(Error::Unauthorized)
    ));
    for prefix in 0..transcript.len() {
        assert!(
            host.answer_native_fresh_join(
                CONFIRM,
                &offer.identity,
                &offer.signature_key,
                &transcript[..prefix],
                NOW
            )
            .is_err()
        );
    }
    let value: Value = coset::cbor::de::from_reader(transcript.as_slice()).unwrap();
    let Value::Array(fields) = value else {
        panic!("fixture canonical challenge")
    };
    for (index, replacement) in [
        (0, Value::Text("arbitrary plaintext".into())),
        (1, Value::Text("https://wrong.example".into())),
        (2, Value::Text("wrong-community".into())),
        (3, Value::Bytes(vec![0; 32])),
        (4, Value::Integer(99u64.into())),
        (5, Value::Bytes(vec![0; 32])),
        (6, Value::Bytes(vec![0; 31])),
    ] {
        let mut wrong = fields.clone();
        wrong[index] = replacement;
        let mut wire = Vec::new();
        coset::cbor::ser::into_writer(&Value::Array(wrong), &mut wire).unwrap();
        assert!(
            host.answer_native_fresh_join(
                CONFIRM,
                &offer.identity,
                &offer.signature_key,
                &wire,
                NOW
            )
            .is_err()
        );
    }
    assert!(
        host.answer_native_fresh_join(
            CONFIRM,
            &offer.identity,
            &offer.signature_key,
            &[0x81; 64],
            NOW
        )
        .is_err()
    );
    assert_eq!(
        host.core
            .connection
            .query_row("SELECT COUNT(*) FROM core_outbox", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        2
    );
    assert_eq!(host.core.phase(), Phase::Live);
    let protected = host
        .answer_native_fresh_join(
            CONFIRM,
            &offer.identity,
            &offer.signature_key,
            &transcript,
            NOW,
        )
        .unwrap();
    assert!(
        host.pending_chat_for_native_publish(CONFIRM, NOW)
            .unwrap()
            .is_none()
    );
    assert_eq!(
        host.pending_native_fresh_join(CONFIRM, NOW)
            .unwrap()
            .unwrap(),
        protected
    );
    peer.confirm_peer(&protected, NOW).unwrap();
    let mut peer = Sdk::bind_native(peer, binding("bob"), NOW).unwrap();
    let wire = host
        .send_chat(CHAT, "fresh-root real MLS protected chat", NOW)
        .unwrap();
    assert_eq!(
        host.pending_chat_for_native_publish(CHAT, NOW)
            .unwrap()
            .unwrap(),
        wire
    );
    let received = peer.receive_chat(CHAT, &wire, NOW).unwrap();
    assert_eq!(received.account, "alice");
    assert_eq!(received.device, "desktop");
    assert_eq!(received.body, "fresh-root real MLS protected chat");
    let reply = "66666666-6666-4666-8666-666666666666";
    let wire = peer.send_chat(reply, "peer reply", NOW).unwrap();
    let received = host.receive_chat(reply, &wire, NOW).unwrap();
    assert_eq!(received.account, "bob");
    assert_eq!(received.body, "peer reply");
    assert_eq!(host.native_owner_facts(NOW).unwrap().epoch(), 1);
    let actual_target = host.native_peer_for_removal("bob", "desktop", NOW).unwrap();
    assert_eq!(actual_target.account(), "bob");
    assert_eq!(actual_target.device(), "desktop");
    assert_eq!(actual_target.identity(), offer.identity);
    assert_eq!(actual_target.signature_key(), offer.signature_key);
    host.check_native_peer_for_removal(&actual_target, NOW)
        .unwrap();
    assert!(matches!(
        peer.check_native_peer_for_removal(&actual_target, NOW),
        Err(Error::Stale)
    ));
    assert!(
        host.native_peer_for_removal("alice", "desktop", NOW)
            .is_err()
    );
    assert!(
        host.native_peer_for_removal("outsider", "desktop", NOW)
            .is_err()
    );
    let mut retained = if matches!(
        scenario,
        RemovalScenario::RetainedPeer
            | RemovalScenario::SuppressedRetainedControl
            | RemovalScenario::SuppressedRetainedState
    ) {
        let keys = if child.is_some() {
            FixtureSeeds(Mutex::new(HashMap::new()), Some(220))
        } else {
            FixtureSeeds::fresh()
        };
        let device = Device::provision_native(&keys, native_pin.clone()).unwrap();
        keys.create_seed("database-key").unwrap();
        let root = host.native_owner_facts(NOW).unwrap();
        let pin = NativeBootstrap::from_native_pin(
            scope(),
            root.group(),
            native_pin.authority_key(),
            root.identity(),
            root.signature_key().try_into().unwrap(),
        )
        .unwrap();
        let (mut third, offer) = Core::begin_native_enrollment(
            &path.join("retained.sqlite"),
            &keys,
            pin,
            b"carol-native",
        )
        .unwrap();
        let invitation = issuer
            .invite_native_reviewed_device(
                NativeAdmissionIntent::from_native_out_of_band_pin(
                    "carol",
                    "desktop",
                    &offer.identity,
                    offer.signature_key.clone().try_into().unwrap(),
                )
                .unwrap(),
                NOW,
            )
            .unwrap();
        let roster = issuer
            .admit_proven_device(&device.respond(&invitation, NOW).unwrap(), NOW)
            .unwrap();
        host.install_native_roster(&roster, NOW).unwrap();
        peer.install_native_roster(&roster, NOW).unwrap();
        let admitted = host
            .add_root_approved_native_peer(
                "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
                "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
                &offer.key_package.tls_serialize_detached().unwrap(),
                NOW,
            )
            .unwrap();
        let stage = peer.stage_native_commit(&admitted.commit, NOW).unwrap();
        peer.inspect_native_commit(stage).unwrap();
        let approval = peer.authorize_native_commit(stage, NOW).unwrap();
        peer.merge_native_commit(approval, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", NOW)
            .unwrap();
        third.install_roster(&roster, NOW).unwrap();
        third.accept_welcome(&admitted.welcome, NOW).unwrap();
        let challenge = third.peer_challenge_transcript().unwrap();
        let proof = host
            .answer_native_fresh_join(
                "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
                &offer.identity,
                &offer.signature_key,
                &challenge,
                NOW,
            )
            .unwrap();
        third.confirm_peer(&proof, NOW).unwrap();
        Some(Sdk::bind_native(third, binding("carol"), NOW).unwrap())
    } else {
        None
    };
    let old_epoch = if retained.is_some() { 2 } else { 1 };
    let old_generation = if retained.is_some() { 3 } else { 2 };
    let actual_target = host.native_peer_for_removal("bob", "desktop", NOW).unwrap();
    // Actual root-signed device withdrawal creates a fresh MLS epoch and its
    // genuine public Remove commit must retire the removed member's MLS state.
    let withdraw_when = if matches!(scenario, RemovalScenario::ExpiredRoster) {
        NOW + 2 * 86400
    } else {
        NOW + 1
    };
    let removal = "77777777-7777-4777-8777-777777777777";
    let future = "88888888-8888-4888-8888-888888888888";
    let old_frame = if matches!(scenario, RemovalScenario::BackwardsTime) {
        let lease = host
            .reserve_source(
                "99999999-9999-4999-8999-999999999999",
                "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
                SourceKind::Opus,
                NOW,
            )
            .unwrap();
        let grant = host.activate_native_frame_bridge(lease, NOW).unwrap();
        assert!(
            host.transform_native_frame(&grant, &[0xf8, 1, 2], NOW)
                .is_ok()
        );
        Some(grant)
    } else {
        None
    };
    let withdrawn = issuer
        .revoke_exact_native_device(
            NativeAdmissionIntent::from_native_out_of_band_pin(
                "bob",
                "desktop",
                &offer.identity,
                offer.signature_key.clone().try_into().unwrap(),
            )
            .unwrap(),
            withdraw_when,
        )
        .unwrap();
    let before_revision = host.core.revision;
    let before_outbox: i64 = host
        .core
        .connection
        .query_row("SELECT COUNT(*) FROM core_outbox", [], |r| r.get(0))
        .unwrap();
    if matches!(scenario, RemovalScenario::SuppressedOutbox) {
        host.core.connection.execute_batch("CREATE TEMP TRIGGER suppress_removal BEFORE INSERT ON core_outbox BEGIN SELECT RAISE(IGNORE); END;").unwrap();
    }
    let (target_identity, target_key) = if matches!(scenario, RemovalScenario::SelfTarget) {
        (own.identity(), own.signature_key())
    } else {
        (offer.identity.as_slice(), offer.signature_key.as_slice())
    };
    let wrong_key = [0u8; 32];
    let target_key = if matches!(scenario, RemovalScenario::WrongKey) {
        wrong_key.as_slice()
    } else {
        target_key
    };
    let removal_roster = if matches!(scenario, RemovalScenario::OldRoster) {
        roster.as_slice()
    } else {
        withdrawn.as_slice()
    };
    let result = host.remove_root_revoked_native_peer(
        removal,
        target_identity,
        target_key,
        removal_roster,
        if matches!(scenario, RemovalScenario::BackwardsTime) {
            NOW - 1
        } else {
            withdraw_when
        },
    );
    if matches!(
        scenario,
        RemovalScenario::SelfTarget
            | RemovalScenario::WrongKey
            | RemovalScenario::OldRoster
            | RemovalScenario::SuppressedOutbox
            | RemovalScenario::BackwardsTime
    ) {
        assert!(result.is_err());
        assert_eq!(host.core.phase(), Phase::Quarantined);
        assert!(host.native_owner_facts(withdraw_when).is_err());
        assert!(!host.matches_native_actor("profile", "main", "session"));
        assert!(
            host.pending_chat_for_native_publish(CHAT, withdraw_when)
                .is_err()
        );
        if let Some(grant) = old_frame {
            assert!(
                host.check_native_frame_bridge(&grant, withdraw_when)
                    .is_err()
            );
            assert!(
                host.transform_native_frame(&grant, &[0xf8, 1, 2], withdraw_when)
                    .is_err()
            );
        }

        let (generation, _) = stored_floor(&host.core.connection).unwrap();
        assert_eq!(generation, 2);
        assert_eq!(host.core.revision, before_revision);
        let after_outbox: i64 = host
            .core
            .connection
            .query_row("SELECT COUNT(*) FROM core_outbox", [], |r| r.get(0))
            .unwrap();
        assert_eq!(after_outbox, before_outbox);
        assert!(host.core.pending(removal).unwrap().is_none());
        return;
    }
    if std::env::var("MNEMA_HOST_FAULT")
        .as_deref()
        .is_ok_and(|f| f.starts_with("remove-ignore-"))
    {
        assert!(result.is_err());
        assert_eq!(host.core.phase(), Phase::Quarantined);
        assert_eq!(stored_floor(&host.core.connection).unwrap().0, 2);
        assert_eq!(host.core.revision, before_revision);
        assert!(host.core.pending(removal).unwrap().is_none());
        return;
    }
    let commit = result.unwrap();
    assert!(matches!(
        host.check_native_peer_for_removal(&actual_target, withdraw_when),
        Err(Error::Stale)
    ));
    assert_eq!(
        host.native_owner_facts(withdraw_when).unwrap().epoch(),
        old_epoch + 1
    );
    assert_eq!(
        host.native_owner_facts(withdraw_when).unwrap().generation(),
        old_generation + 1
    );
    assert_eq!(
        host.core.group.as_ref().unwrap().members().count(),
        if retained.is_some() { 2 } else { 1 }
    );
    assert_eq!(
        host.pending_native_host_publication(removal, withdraw_when)
            .unwrap()
            .unwrap(),
        commit
    );
    assert!(matches!(
        host.pending_native_host_publication(COMMIT, withdraw_when),
        Err(Error::Stale)
    ));
    let new_wire = host
        .send_chat(future, "future after actual MLS removal", withdraw_when)
        .unwrap();
    // Withholding control bytes yields only an epoch mismatch. This guard
    // assertion alone does not demonstrate a decryption attempt or exclusion.
    assert!(matches!(
        peer.core.receive_inner(&new_wire, withdraw_when),
        Err(Error::Invalid)
    ));
    assert_eq!(
        peer.core.group.as_ref().unwrap().epoch().as_u64(),
        old_epoch
    );
    // Ordinary old membership still approves Bob. The one real atomic control
    // receiver must verify the freshly signed root roster and genuine MLS Remove.
    if !matches!(scenario, RemovalScenario::ExpiredRoster) {
        let (mut trust, roster, generation) =
            current_trust(&peer.core.connection, &peer.core.pin, withdraw_when).unwrap();
        assert_eq!(generation, old_generation);
        let approved = trust
            .verify_device(
                &roster,
                &peer.core.pin.group,
                &offer.identity,
                &offer.signature_key,
                withdraw_when,
            )
            .unwrap();
        assert_eq!(approved.account(), "bob");
    }
    let control = host
        .pending_native_device_removal(removal, withdraw_when)
        .unwrap()
        .unwrap();
    let observed = host
        .archived_device_removal("alice", removal, &control)
        .unwrap()
        .unwrap();
    assert!(!observed.removed_self());
    assert_eq!(observed.epoch(), old_epoch + 1);
    if matches!(
        scenario,
        RemovalScenario::SuppressedReceiverControl
            | RemovalScenario::SuppressedReceiverState
            | RemovalScenario::SuppressedRetainedControl
            | RemovalScenario::SuppressedRetainedState
    ) {
        let receiver = retained.as_mut().unwrap_or(&mut peer);
        let group_id = receiver.core.group.as_ref().unwrap().group_id().clone();
        let revision = receiver.core.revision;
        let floor = stored_floor(&receiver.core.connection).unwrap();
        let suppress_state = matches!(
            scenario,
            RemovalScenario::SuppressedReceiverState | RemovalScenario::SuppressedRetainedState
        );
        receiver.core.connection.execute_batch(if suppress_state {
            "CREATE TRIGGER suppress_receiver BEFORE UPDATE ON core_state BEGIN SELECT RAISE(IGNORE);END;"
        } else {
            "CREATE TRIGGER suppress_receiver BEFORE INSERT ON core_device_removals BEGIN SELECT RAISE(IGNORE);END;"
        }).unwrap();
        assert!(
            receiver
                .receive_native_device_removal("alice", removal, &control, withdraw_when)
                .is_err()
        );
        assert!(receiver.core.group.is_none());
        assert_eq!(receiver.core.revision, revision);
        assert_eq!(stored_floor(&receiver.core.connection).unwrap(), floor);
        let rows: i64 = receiver
            .core
            .connection
            .query_row("SELECT COUNT(*) FROM core_device_removals", [], |r| {
                r.get(0)
            })
            .unwrap();
        assert_eq!(rows, 0);
        let provider = Provider::new(&receiver.core.connection, &receiver.core.crypto);
        let persisted = MlsGroup::load(provider.storage(), &group_id)
            .unwrap()
            .unwrap();
        assert!(persisted.is_active());
        assert_eq!(persisted.epoch().as_u64(), old_epoch);
        return;
    }
    if matches!(
        scenario,
        RemovalScenario::RelabeledControl
            | RemovalScenario::WrongControlAccount
            | RemovalScenario::WrongControlChannel
            | RemovalScenario::TrailingControl
            | RemovalScenario::ReceiverBackwardsTime
    ) {
        if matches!(scenario, RemovalScenario::WrongControlChannel) {
            let wrong = NativeBinding::from_native_actor(
                "ffffffff-ffff-4fff-8fff-ffffffffffff",
                "profile",
                "main",
                "session",
                "bob",
                "desktop",
            )
            .unwrap();
            peer = Sdk::bind_native(peer.core, wrong, NOW).unwrap();
        }
        let revision = peer.core.revision;
        if matches!(scenario, RemovalScenario::ReceiverBackwardsTime) {
            let durable_time = stored_floor(&peer.core.connection).unwrap().1;
            peer.native_owner_facts(withdraw_when + 100).unwrap();
            assert_eq!(stored_floor(&peer.core.connection).unwrap().1, durable_time);
            assert!(durable_time < withdraw_when + 100);
        }
        let event = if matches!(scenario, RemovalScenario::RelabeledControl) {
            future
        } else {
            removal
        };
        let account = if matches!(scenario, RemovalScenario::WrongControlAccount) {
            "mallory"
        } else {
            "alice"
        };
        let mut bytes = control.clone();
        if matches!(scenario, RemovalScenario::TrailingControl) {
            bytes.push(0);
        }
        assert!(
            peer.receive_native_device_removal(account, event, &bytes, withdraw_when)
                .is_err()
        );
        assert!(peer.core.group.is_none());
        assert_eq!(peer.core.revision, revision);
        assert_eq!(
            stored_floor(&peer.core.connection).unwrap().0,
            old_generation
        );
        let rows: i64 = peer
            .core
            .connection
            .query_row("SELECT COUNT(*) FROM core_device_removals", [], |r| {
                r.get(0)
            })
            .unwrap();
        assert_eq!(rows, 0);
        return;
    }
    if let Some(retained) = retained.as_mut() {
        let proof = retained
            .receive_native_device_removal("alice", removal, &control, withdraw_when)
            .unwrap();
        assert!(!proof.removed_self());
        assert_eq!(
            retained.native_owner_facts(withdraw_when).unwrap().epoch(),
            old_epoch + 1
        );
        let future_message = retained
            .receive_chat(future, &new_wire, withdraw_when)
            .unwrap();
        assert_eq!(future_message.body, "future after actual MLS removal");
        assert_eq!(future_message.account, "alice");
        let saved = retained
            .archived_device_removal("alice", removal, &control)
            .unwrap()
            .unwrap();
        assert!(
            saved.belongs_to_native_owner(
                &retained
                    .native_protected_event_scope(withdraw_when)
                    .unwrap()
            )
        );
    }
    let proof = peer
        .receive_native_device_removal("alice", removal, &control, withdraw_when)
        .unwrap();
    assert!(proof.removed_self());
    assert_eq!(proof.account(), "alice");
    assert_eq!(proof.epoch(), old_epoch + 1);
    assert_eq!(proof.generation(), old_generation + 1);
    let saved = peer
        .archived_device_removal("alice", removal, &control)
        .unwrap()
        .unwrap();
    assert!(saved.removed_self());
    assert!(
        peer.receive_protected(future, &new_wire, withdraw_when)
            .is_err()
    );
    assert_eq!(peer.core.phase(), Phase::Quarantined);
    assert!(peer.core.group.is_none());
    assert!(peer.core.signer.is_none());
    assert!(peer.native_owner_facts(withdraw_when).is_err());
}
#[test]
fn actual_host_rejects_keypackage_without_signed_root_device_admission_before_merge() {
    let d = dir();
    let root = FixtureSeeds::fresh();
    let outsider = FixtureSeeds::fresh();
    let fresh = FreshCommunity::provision_first_group(
        &d.path().join("root.sqlite"),
        &root,
        scope(),
        subject(),
        NOW,
    )
    .unwrap();
    let (issuer, transfer) = fresh.into_native_host();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&outsider, pin.clone()).unwrap();
    outsider.create_seed("database-key").unwrap();
    let mut host = Sdk::from_fresh_native_host(transfer, binding("alice"), NOW).unwrap();
    let own = host.native_owner_facts(NOW).unwrap();
    let p = NativeBootstrap::from_native_pin(
        scope(),
        own.group(),
        pin.authority_key(),
        own.identity(),
        own.signature_key().try_into().unwrap(),
    )
    .unwrap();
    let (_, offer) = Core::begin_native_enrollment(
        &d.path().join("outsider.sqlite"),
        &outsider,
        p,
        b"spoofed-account",
    )
    .unwrap();
    assert_eq!(offer.signature_key, device.public_key());
    assert!(matches!(
        host.add_root_approved_native_peer(
            COMMIT,
            WELCOME,
            &offer.key_package.tls_serialize_detached().unwrap(),
            NOW
        ),
        Err(Error::Unauthorized)
    ));
    assert_eq!(
        host.core
            .connection
            .query_row("SELECT COUNT(*) FROM core_outbox", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        0
    );
    assert_eq!(host.core.phase(), Phase::Quarantined);
}

#[test]
fn archived_own_event_survives_actual_membership_advance_without_reviving_reservation() {
    let d = dir();
    let root_keys = FixtureSeeds::fresh();
    let peer_keys = FixtureSeeds::fresh();
    let fresh = FreshCommunity::provision_first_group(
        &d.path().join("root.sqlite"),
        &root_keys,
        scope(),
        subject(),
        NOW,
    )
    .unwrap();
    let (mut issuer, transfer) = fresh.into_native_host();
    let mut host = Sdk::from_fresh_native_host(transfer, binding("alice"), NOW).unwrap();
    let claim = ChatEventClaim::claim(ChatOperation::Create {
        message_id: CHAT.into(),
        parent_id: None,
        body: "historical before native membership advance".into(),
    })
    .unwrap();
    let reserved = host.reserve_native_chat_event(CHAT, &claim, NOW).unwrap();
    let old_wire = reserved.wire_for_native_relay().to_vec();
    let old = host
        .archived_chat_observation("alice", CHAT, &old_wire)
        .unwrap()
        .unwrap();
    assert_eq!(old.epoch(), 0);
    assert_eq!(old.root_generation(), 1);
    let native_pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&peer_keys, native_pin.clone()).unwrap();
    peer_keys.create_seed("database-key").unwrap();
    let own = host.native_owner_facts(NOW).unwrap();
    let pin = NativeBootstrap::from_native_pin(
        scope(),
        own.group(),
        native_pin.authority_key(),
        own.identity(),
        own.signature_key().try_into().unwrap(),
    )
    .unwrap();
    let (mut peer, offer) = Core::begin_native_enrollment(
        &d.path().join("peer.sqlite"),
        &peer_keys,
        pin,
        b"bob-native",
    )
    .unwrap();
    let intent = NativeAdmissionIntent::from_native_out_of_band_pin(
        "bob",
        "desktop",
        &offer.identity,
        offer.signature_key.clone().try_into().unwrap(),
    )
    .unwrap();
    let invitation = issuer.invite_native_reviewed_device(intent, NOW).unwrap();
    let roster = issuer
        .admit_proven_device(&device.respond(&invitation, NOW).unwrap(), NOW)
        .unwrap();
    host.install_native_roster(&roster, NOW).unwrap();
    peer.install_roster(&roster, NOW).unwrap();
    host.add_root_approved_native_peer(
        COMMIT,
        WELCOME,
        &offer.key_package.tls_serialize_detached().unwrap(),
        NOW,
    )
    .unwrap();
    let current = host.native_protected_event_scope(NOW).unwrap();
    assert_eq!(current.epoch(), 1);
    assert_eq!(current.root_generation(), 2);
    assert!(matches!(
        host.pending_native_chat_event_projection(&reserved, NOW),
        Err(Error::Stale)
    ));
    let archive = host
        .archived_chat_observation("alice", CHAT, &old_wire)
        .unwrap()
        .unwrap();
    assert!(archive.matches_history_context(&current));
    assert_eq!(archive.epoch(), 0);
    assert_eq!(archive.root_generation(), 1);
    assert!(
        matches!(archive.operation(), ChatOperation::Create { body, .. }
        if body == "historical before native membership advance")
    );
    // Reading history never changes the original reservation's send eligibility.
    assert!(matches!(
        host.pending_native_chat_event_projection(&reserved, NOW),
        Err(Error::Stale)
    ));
}

#[test]
fn actual_host_transaction_crashes_and_suppressed_rows_never_publish_partial_owner_or_admission() {
    for fault in [
        "adopt-before",
        "adopt-after",
        "adopt-ignore-channel",
        "adopt-ignore-state",
        "add-before",
        "add-after",
        "add-ignore-outbox",
        "add-ignore-context",
        "add-ignore-private-epoch",
        "add-ignore-resumption",
        "add-ignore-message-secrets",
    ] {
        let d = dir();
        let status=std::process::Command::new(std::env::current_exe().unwrap()).arg("--exact").arg("host_tests::actual_same_native_device_pairing_host_add_welcome_fresh_peer_and_protected_chat_both_directions").arg("--test-threads=1").env("MNEMA_HOST_CHILD_DIR",d.path()).env("MNEMA_HOST_FAULT",fault).stdout(std::process::Stdio::null()).stderr(std::process::Stdio::null()).status().unwrap();
        assert_eq!(
            status.code(),
            Some(if fault.ends_with("before") || fault.ends_with("after") {
                73
            } else {
                0
            }),
            "fault child expected native boundary"
        );
        struct ChildKeys;
        impl NativeKeyProvider for ChildKeys {
            fn database_key(&self) -> Result<Zeroizing<[u8; 32]>> {
                Ok(Zeroizing::new([202; 32]))
            }
        }
        let c = open_encrypted(&d.path().join("root.sqlite"), &ChildKeys, false).unwrap();
        let core_exists: i64 = c
            .query_row(
                "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='core_state'",
                [],
                |r| r.get(0),
            )
            .unwrap();
        let adopted = fault == "adopt-after" || fault.starts_with("add-");
        assert_eq!(core_exists, i64::from(adopted));
        if adopted {
            let outbox: i64 = c
                .query_row("SELECT COUNT(*) FROM core_outbox", [], |r| r.get(0))
                .unwrap();
            assert_eq!(outbox, if fault == "add-after" { 2 } else { 0 });
            let (o,com,g,a,i,k):(String,String,Vec<u8>,Vec<u8>,Vec<u8>,Vec<u8>)=c.query_row("SELECT origin,community,group_id,authority,peer_identity,peer_key FROM core_pin",[],|r|Ok((r.get(0)?,r.get(1)?,r.get(2)?,r.get(3)?,r.get(4)?,r.get(5)?))).unwrap();
            let pin = NativeBootstrap::from_native_pin(
                Scope::new(&o, &com).unwrap(),
                &g,
                a.try_into().unwrap(),
                &i,
                k.try_into().unwrap(),
            )
            .unwrap();
            let restored =
                Core::restore_inspection_only(&d.path().join("root.sqlite"), &ChildKeys, pin)
                    .unwrap();
            assert_eq!(restored.phase(), Phase::Restored);
            assert!(Sdk::bind_native(restored, binding("alice"), NOW).is_err());
        }
    }
}

#[test]
fn actual_local_authority_generation_rejects_old_live_floor_and_durable_pending_ciphertext() {
    let d = dir();
    let keys = FixtureSeeds::fresh();
    let fresh = FreshCommunity::provision_first_group(
        &d.path().join("root.sqlite"),
        &keys,
        scope(),
        subject(),
        NOW,
    )
    .unwrap();
    let (mut issuer, transfer) = fresh.into_native_host();
    let mut sdk = Sdk::from_fresh_native_host(transfer, binding("alice"), NOW).unwrap();
    let owner = sdk.native_owner_facts(NOW).unwrap();
    sdk.send_chat(CHAT, "already durable protected text", NOW)
        .unwrap();
    issuer
        .revoke_exact_native_device(
            NativeAdmissionIntent::from_native_out_of_band_pin(
                "alice",
                "desktop",
                owner.identity(),
                owner.signature_key().try_into().unwrap(),
            )
            .unwrap(),
            NOW,
        )
        .unwrap();
    assert_eq!(
        sdk.pending_chat_for_native_publish(CHAT, NOW),
        Err(Error::Stale)
    );
    assert!(matches!(sdk.native_owner_facts(NOW), Err(Error::Stale)));
    assert_eq!(
        sdk.core
            .connection
            .query_row("SELECT COUNT(*) FROM core_outbox", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        1
    );
}
#[test]
fn actual_sealed_host_persists_channel_and_rejects_later_native_binding_substitution() {
    let d = dir();
    let keys = FixtureSeeds::fresh();
    let fresh = FreshCommunity::provision_first_group(
        &d.path().join("root.sqlite"),
        &keys,
        scope(),
        subject(),
        NOW,
    )
    .unwrap();
    let (_, transfer) = fresh.into_native_host();
    let core = Core::adopt_fresh_native_host(transfer, CHANNEL, NOW + 100).unwrap();
    assert_eq!(
        core.connection
            .query_row(
                "SELECT observed_time FROM issuer_state WHERE id=1",
                [],
                |r| r.get::<_, i64>(0)
            )
            .unwrap(),
        (NOW + 100) as i64
    );
    let wrong = NativeBinding::from_native_actor(
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
        "profile",
        "main",
        "session",
        "alice",
        "desktop",
    )
    .unwrap();
    assert!(matches!(
        Sdk::bind_native(core, wrong, NOW + 100),
        Err(Error::Trust)
    ));
}

#[test]
fn actual_suppressed_sender_private_ratchet_never_releases_ciphertext_or_retries_live_owner() {
    let d = dir();
    let keys = FixtureSeeds::fresh();
    let fresh = FreshCommunity::provision_first_group(
        &d.path().join("root.sqlite"),
        &keys,
        scope(),
        subject(),
        NOW,
    )
    .unwrap();
    let (_, transfer) = fresh.into_native_host();
    let mut sdk = Sdk::from_fresh_native_host(transfer, binding("alice"), NOW).unwrap();
    let before: Vec<u8> = sdk
        .core
        .connection
        .query_row(
            "SELECT group_data FROM openmls_group_data WHERE data_type='message_secrets'",
            [],
            |r| r.get(0),
        )
        .unwrap();
    sdk.core.connection.execute_batch("CREATE TEMP TRIGGER suppress_private_ratchet BEFORE INSERT ON openmls_group_data WHEN NEW.data_type='message_secrets' BEGIN SELECT RAISE(IGNORE);END;").unwrap();
    assert!(matches!(
        sdk.send_chat(CHAT, "blocked fixture body", NOW),
        Err(Error::Database | Error::Provider)
    ));
    assert_eq!(sdk.core.phase(), Phase::Quarantined);
    assert_eq!(
        sdk.core
            .connection
            .query_row("SELECT COUNT(*) FROM core_outbox", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        0
    );
    assert_eq!(
        sdk.core
            .connection
            .query_row("SELECT COUNT(*) FROM sdk_events", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        0
    );
    let after: Vec<u8> = sdk
        .core
        .connection
        .query_row(
            "SELECT group_data FROM openmls_group_data WHERE data_type='message_secrets'",
            [],
            |r| r.get(0),
        )
        .unwrap();
    assert_eq!(before, after);
    sdk.core
        .connection
        .execute_batch("DROP TRIGGER suppress_private_ratchet;")
        .unwrap();
    assert!(sdk.send_chat(CHAT, "never retry encrypt", NOW).is_err());
    assert!(sdk.pending_chat_for_native_publish(CHAT, NOW).is_err());
}

const COMMUNITY_ACCOUNT: &str = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const COMMUNITY_DEVICE: &str = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
fn community_sdk(keys: &FixtureSeeds, path: &Path) -> Sdk {
    let identity = format!("{COMMUNITY_ACCOUNT}.{COMMUNITY_DEVICE}");
    let subject = NativeAdminSubject::from_native_admin_workflow(
        COMMUNITY_ACCOUNT,
        COMMUNITY_DEVICE,
        identity.as_bytes(),
    )
    .unwrap();
    let fresh = FreshCommunity::provision_first_group(path, keys, scope(), subject, NOW).unwrap();
    let (_, transfer) = fresh.into_native_host();
    let binding = NativeBinding::from_native_actor(
        CHANNEL,
        "profile",
        "main",
        "session",
        COMMUNITY_ACCOUNT,
        COMMUNITY_DEVICE,
    )
    .unwrap();
    Sdk::from_fresh_native_host(transfer, binding, NOW).unwrap()
}
fn signed_voice_creation(
    keys: &FixtureSeeds,
    authorization: &VerifiedCommunityAuthorization,
) -> Vec<u8> {
    let fields = Value::Array(vec![
        Value::Text("MnemaTalk VoiceGroupCreation".into()),
        Value::Integer(1.into()),
        Value::Text(scope().origin().into()),
        Value::Text(scope().community().into()),
        Value::Text(CHANNEL.into()),
        Value::Text("cccccccc-cccc-4ccc-8ccc-cccccccccccc".into()),
        Value::Bytes(vec![37; 32]),
        Value::Text(COMMUNITY_ACCOUNT.into()),
        Value::Text(COMMUNITY_DEVICE.into()),
        Value::Integer(authorization.generation().into()),
        Value::Bytes(authorization.digest().to_vec()),
        Value::Integer(NOW.into()),
        Value::Integer((NOW + 200).into()),
    ]);
    let mut payload = Vec::new();
    coset::cbor::ser::into_writer(&fields, &mut payload).unwrap();
    use ed25519_dalek::Signer;
    let seed = keys.read_seed("device-key").unwrap();
    let signer = ed25519_dalek::SigningKey::from_bytes(&seed);
    coset::CoseSign1Builder::new()
        .protected(
            coset::HeaderBuilder::new()
                .algorithm(coset::iana::Algorithm::Ed25519)
                .build(),
        )
        .payload(payload)
        .create_signature(b"MnemaTalk VoiceGroupCreation/v1", |t| {
            signer.sign(t).to_bytes().to_vec()
        })
        .build()
        .to_tagged_vec()
        .unwrap()
}
#[test]
fn community_actual_root_key_and_admitted_device_issuance_durable_idempotence_and_time_floor() {
    let d = dir();
    let keys = FixtureSeeds::fresh();
    let mut sdk = community_sdk(&keys, &d.path().join("community.sqlite"));
    let wire = sdk
        .issue_native_community_authorization(&keys, NOW)
        .unwrap();
    let author = sdk
        .install_native_community_authorization(&wire, NOW)
        .unwrap();
    assert_eq!(author.generation(), 1);
    assert_eq!(author.device_count(), 1);
    assert_eq!(author.origin(), scope().origin());
    assert_eq!(author.community(), scope().community());
    assert_eq!(
        sdk.issue_native_community_authorization(&keys, NOW + 1)
            .unwrap(),
        wire
    );
    let creation = signed_voice_creation(&keys, &author);
    let verified = sdk
        .verify_native_voice_creation(&author, &creation, NOW + 10)
        .unwrap();
    assert_eq!(verified.channel(), CHANNEL);
    assert_eq!(verified.group(), &[37; 32]);
    assert_eq!(verified.creator_account(), COMMUNITY_ACCOUNT);
    assert_eq!(verified.creator_device(), COMMUNITY_DEVICE);
    assert_eq!(
        verified.room_incarnation(),
        "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
    );
    let floor: i64 = sdk
        .core
        .connection
        .query_row(
            "SELECT observed_time FROM sdk_community_authorization WHERE id=1",
            [],
            |r| r.get(0),
        )
        .unwrap();
    assert_eq!(floor, (NOW + 10) as i64);
    assert!(
        sdk.verify_native_voice_creation(&author, &creation, NOW + 9)
            .is_err()
    );
    assert!(!sdk.matches_native_actor("profile", "main", "session"));
}
#[test]
fn community_suppressed_floor_insert_or_update_has_no_authorization_output_and_retires() {
    for update in [false, true] {
        let d = dir();
        let keys = FixtureSeeds::fresh();
        let mut sdk = community_sdk(&keys, &d.path().join("community-fault.sqlite"));
        let wire = if update {
            Some(
                sdk.issue_native_community_authorization(&keys, NOW)
                    .unwrap(),
            )
        } else {
            None
        };
        sdk.core.connection.execute_batch("CREATE TABLE IF NOT EXISTS sdk_community_authorization(id INTEGER PRIMARY KEY,generation BLOB,digest BLOB,wire BLOB,observed_time INTEGER);").unwrap();
        sdk.core.connection.execute_batch(if update{"CREATE TRIGGER suppress BEFORE UPDATE ON sdk_community_authorization BEGIN SELECT RAISE(IGNORE);END;"}else{"CREATE TRIGGER suppress BEFORE INSERT ON sdk_community_authorization BEGIN SELECT RAISE(IGNORE);END;"}).unwrap();
        let result = if let Some(wire) = wire {
            sdk.install_native_community_authorization(&wire, NOW + 1)
                .map(|_| ())
        } else {
            sdk.issue_native_community_authorization(&keys, NOW)
                .map(|_| ())
        };
        assert_eq!(result, Err(Error::Database));
        assert!(!sdk.matches_native_actor("profile", "main", "session"));
        let count: i64 = sdk
            .core
            .connection
            .query_row(
                "SELECT COUNT(*) FROM sdk_community_authorization",
                [],
                |r| r.get(0),
            )
            .unwrap();
        assert_eq!(count, if update { 1 } else { 0 });
        if update {
            let last: i64 = sdk
                .core
                .connection
                .query_row(
                    "SELECT observed_time FROM sdk_community_authorization WHERE id=1",
                    [],
                    |r| r.get(0),
                )
                .unwrap();
            assert_eq!(last, NOW as i64)
        }
    }
}
#[test]
fn community_creation_cannot_cross_sdk_owner_and_same_generation_changed_root_wire_rejects() {
    let d = dir();
    let keys = FixtureSeeds::fresh();
    let mut sdk = community_sdk(&keys, &d.path().join("community-owner.sqlite"));
    let wire = sdk
        .issue_native_community_authorization(&keys, NOW)
        .unwrap();
    let author = sdk
        .install_native_community_authorization(&wire, NOW)
        .unwrap();
    let creation = signed_voice_creation(&keys, &author);
    let other_keys = FixtureSeeds::fresh();
    let mut other = community_sdk(&other_keys, &d.path().join("foreign.sqlite"));
    let other_wire = other
        .issue_native_community_authorization(&other_keys, NOW)
        .unwrap();
    other
        .install_native_community_authorization(&other_wire, NOW)
        .unwrap();
    assert!(
        other
            .verify_native_voice_creation(&author, &creation, NOW)
            .is_err()
    );
    assert!(!other.matches_native_actor("profile", "main", "session"));
    let envelope = CoseSign1::from_tagged_slice(&wire).unwrap();
    let mut payload: Value =
        coset::cbor::de::from_reader(envelope.payload.unwrap().as_slice()).unwrap();
    let Value::Array(fields) = &mut payload else {
        panic!("fixture")
    };
    fields[6] = Value::Integer((NOW + 600).into());
    let mut encoded = Vec::new();
    coset::cbor::ser::into_writer(&payload, &mut encoded).unwrap();
    use ed25519_dalek::Signer;
    let seed = keys.read_seed("issuer-root").unwrap();
    let root = ed25519_dalek::SigningKey::from_bytes(&seed);
    let changed = coset::CoseSign1Builder::new()
        .protected(
            coset::HeaderBuilder::new()
                .algorithm(coset::iana::Algorithm::Ed25519)
                .build(),
        )
        .payload(encoded)
        .create_signature(b"MnemaTalk CommunityAuthorization/v1", |t| {
            root.sign(t).to_bytes().to_vec()
        })
        .build()
        .to_tagged_vec()
        .unwrap();
    assert!(matches!(
        sdk.install_native_community_authorization(&changed, NOW),
        Err(Error::Replay)
    ));
    let exact: bool = sdk
        .core
        .connection
        .query_row(
            "SELECT wire=? FROM sdk_community_authorization WHERE id=1",
            [wire.as_slice()],
            |r| r.get(0),
        )
        .unwrap();
    assert!(exact);
}

#[test]
fn community_new_generation_retires_old_approval_and_own_revocation_persists_before_denial() {
    for revoke_own in [false, true] {
        let d = dir();
        let keys = FixtureSeeds::fresh();
        let mut sdk = community_sdk(&keys, &d.path().join("community-revoke.sqlite"));
        let original = sdk
            .issue_native_community_authorization(&keys, NOW)
            .unwrap();
        let author = sdk
            .install_native_community_authorization(&original, NOW)
            .unwrap();
        let proposal = signed_voice_creation(&keys, &author);
        let envelope = CoseSign1::from_tagged_slice(&original).unwrap();
        let mut payload: Value =
            coset::cbor::de::from_reader(envelope.payload.unwrap().as_slice()).unwrap();
        let Value::Array(fields) = &mut payload else {
            panic!("fixture")
        };
        fields[4] = Value::Integer(2.into());
        if revoke_own {
            let foreign_account = "cccccccc-cccc-4ccc-8ccc-cccccccccccc";
            let foreign_device = "dddddddd-dddd-4ddd-8ddd-dddddddddddd";
            let foreign = ed25519_dalek::SigningKey::from_bytes(&[53; 32]);
            fields[7] = Value::Array(vec![Value::Array(vec![
                Value::Text(foreign_account.into()),
                Value::Text(foreign_device.into()),
                Value::Bytes(format!("{foreign_account}.{foreign_device}").into_bytes()),
                Value::Bytes(foreign.verifying_key().to_bytes().to_vec()),
                Value::Text("voice-group-v1".into()),
            ])]);
        }
        let mut encoded = Vec::new();
        coset::cbor::ser::into_writer(&payload, &mut encoded).unwrap();
        use ed25519_dalek::Signer;
        let seed = keys.read_seed("issuer-root").unwrap();
        let root = ed25519_dalek::SigningKey::from_bytes(&seed);
        // Root-signed fixture update emulates a future separately qualified
        // authority issuer update, not a server/admin role or renderer grant.
        let newer = coset::CoseSign1Builder::new()
            .protected(
                coset::HeaderBuilder::new()
                    .algorithm(coset::iana::Algorithm::Ed25519)
                    .build(),
            )
            .payload(encoded)
            .create_signature(b"MnemaTalk CommunityAuthorization/v1", |t| {
                root.sign(t).to_bytes().to_vec()
            })
            .build()
            .to_tagged_vec()
            .unwrap();
        let result = sdk.install_native_community_authorization(&newer, NOW + 1);
        if revoke_own {
            assert!(matches!(result, Err(Error::Unauthorized)));
            assert!(!sdk.matches_native_actor("profile", "main", "session"));
        } else {
            assert_eq!(result.unwrap().generation(), 2);
            assert!(
                sdk.verify_native_voice_creation(&author, &proposal, NOW + 1)
                    .is_err()
            );
        }
        let generation: Vec<u8> = sdk
            .core
            .connection
            .query_row(
                "SELECT generation FROM sdk_community_authorization WHERE id=1",
                [],
                |r| r.get(0),
            )
            .unwrap();
        assert_eq!(generation, 2u64.to_be_bytes());
        let exact: bool = sdk
            .core
            .connection
            .query_row(
                "SELECT wire=? FROM sdk_community_authorization WHERE id=1",
                [newer.as_slice()],
                |r| r.get(0),
            )
            .unwrap();
        assert!(exact);
    }
}

#[test]
fn actual_host_removal_crashes_and_suppressed_writes_are_atomic() {
    for fault in [
        "remove-before",
        "remove-after",
        "remove-ignore-state",
        "remove-ignore-outbox",
        "remove-ignore-events",
        "remove-ignore-control",
        "remove-ignore-context",
        "remove-ignore-private-epoch",
        "remove-ignore-resumption",
        "remove-ignore-message-secrets",
    ] {
        let d = dir();
        let status = std::process::Command::new(std::env::current_exe().unwrap())
            .args(["--exact", "host_tests::actual_same_native_device_pairing_host_add_welcome_fresh_peer_and_protected_chat_both_directions", "--test-threads=1"])
            .env("MNEMA_HOST_CHILD_DIR", d.path()).env("MNEMA_HOST_FAULT", fault)
            .stdout(std::process::Stdio::null()).stderr(std::process::Stdio::null()).status().unwrap();
        assert_eq!(
            status.code(),
            Some(if fault.ends_with("before") || fault.ends_with("after") {
                73
            } else {
                0
            }),
            "{fault}"
        );
        struct ChildKeys;
        impl NativeKeyProvider for ChildKeys {
            fn database_key(&self) -> Result<Zeroizing<[u8; 32]>> {
                Ok(Zeroizing::new([202; 32]))
            }
        }
        let c = open_encrypted(&d.path().join("root.sqlite"), &ChildKeys, false).unwrap();
        let committed = fault == "remove-after";
        assert_eq!(stored_floor(&c).unwrap().0, if committed { 3 } else { 2 });
        let removal = "77777777-7777-4777-8777-777777777777";
        for table_column in [
            "core_outbox WHERE event_id=?",
            "sdk_events WHERE event=?",
            "core_device_removals WHERE event=?",
        ] {
            let count: i64 = c
                .query_row(
                    &format!("SELECT COUNT(*) FROM {table_column}"),
                    [removal],
                    |r| r.get(0),
                )
                .unwrap();
            assert_eq!(count, i64::from(committed), "{fault}");
        }
        let (o, com, g, a, i, k): (String, String, Vec<u8>, Vec<u8>, Vec<u8>, Vec<u8>) = c
            .query_row(
                "SELECT origin,community,group_id,authority,peer_identity,peer_key FROM core_pin",
                [],
                |r| {
                    Ok((
                        r.get(0)?,
                        r.get(1)?,
                        r.get(2)?,
                        r.get(3)?,
                        r.get(4)?,
                        r.get(5)?,
                    ))
                },
            )
            .unwrap();
        let crypto = RustCrypto::default();
        let provider = Provider::new(&c, &crypto);
        let group = MlsGroup::load(provider.storage(), &GroupId::from_slice(&g))
            .unwrap()
            .unwrap();
        assert_eq!(group.epoch().as_u64(), if committed { 2 } else { 1 });
        assert_eq!(group.members().count(), if committed { 1 } else { 2 });
        let issuer_generation: i64 = c
            .query_row("SELECT generation FROM issuer_state WHERE id=1", [], |r| {
                r.get(0)
            })
            .unwrap();
        assert_eq!(issuer_generation, 3); // Root withdrawal remains durable.
        let pin = NativeBootstrap::from_native_pin(
            Scope::new(&o, &com).unwrap(),
            &g,
            a.try_into().unwrap(),
            &i,
            k.try_into().unwrap(),
        )
        .unwrap();
        let restored =
            Core::restore_inspection_only(&d.path().join("root.sqlite"), &ChildKeys, pin).unwrap();
        assert_eq!(restored.phase(), Phase::Restored);
        assert!(Sdk::bind_native(restored, binding("alice"), NOW + 1).is_err());
    }
}
