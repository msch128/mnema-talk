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
    let (mut peer, offer) =
        Core::begin_native_enrollment(&path.join("peer.sqlite"), &peer_keys, pin, b"bob-native")
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
