use super::*;
use openmls::prelude::tls_codec::Deserialize;
use std::{collections::HashMap, path::PathBuf, sync::Mutex};
const NOW: u64 = 1000;
struct MemorySeeds {
    values: Mutex<HashMap<String, Zeroizing<[u8; 32]>>>,
}
impl MemorySeeds {
    fn fresh() -> Self {
        Self {
            values: Mutex::new(HashMap::new()),
        }
    }
    fn seed(alias: &str) -> [u8; 32] {
        match alias {
            "issuer-root" => [11; 32],
            "database-key" => [12; 32],
            "device-key" => [13; 32],
            _ => [0; 32],
        }
    }
    fn restored_public_fixture() -> Self {
        let s = Self::fresh();
        for a in ["issuer-root", "database-key", "device-key"] {
            s.values
                .lock()
                .unwrap()
                .insert(a.into(), Zeroizing::new(Self::seed(a)));
        }
        s
    }
}
impl NativeSecrets for MemorySeeds {
    fn read_seed(
        &self,
        alias: &str,
    ) -> mnema_crypto_enrollment_prototype::Result<Zeroizing<[u8; 32]>> {
        self.values
            .lock()
            .unwrap()
            .get(alias)
            .cloned()
            .ok_or(mnema_crypto_enrollment_prototype::Error::NativeUnavailable)
    }
    fn create_seed(&self, alias: &str) -> mnema_crypto_enrollment_prototype::Result<[u8; 32]> {
        let mut values = self.values.lock().unwrap();
        if values.contains_key(alias) {
            return Err(mnema_crypto_enrollment_prototype::Error::NativeUnavailable);
        }
        let seed = Self::seed(alias);
        let public = ed25519_dalek::SigningKey::from_bytes(&seed)
            .verifying_key()
            .to_bytes();
        values.insert(alias.into(), Zeroizing::new(seed));
        Ok(public)
    }
}
fn scope() -> Scope {
    Scope::new("https://bootstrap.example", "fixture-community").unwrap()
}
fn subject() -> NativeAdminSubject {
    NativeAdminSubject::from_native_admin_workflow(
        "admin-account",
        "desktop",
        b"native-admin-device",
    )
    .unwrap()
}
fn dir() -> tempfile::TempDir {
    let root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("evidence/runs");
    std::fs::create_dir_all(&root).unwrap();
    tempfile::Builder::new()
        .prefix("bootstrap-private-")
        .tempdir_in(root)
        .unwrap()
}
fn pin(public: &PublicBootstrap) -> NativeRootPin {
    NativeRootPin::from_out_of_band(scope(), &public.group, public.authority).unwrap()
}
#[test]
fn actual_first_group_root_roster_device_possession_same_signer_and_exact_outbox() {
    let d = dir();
    let seeds = MemorySeeds::fresh();
    let fresh = FreshCommunity::provision_first_group(
        &d.path().join("root.sqlite"),
        &seeds,
        scope(),
        subject(),
        NOW,
    )
    .unwrap();
    let p = fresh.public_for_native_oob_ceremony();
    assert_eq!(fresh.inspect_provider_epoch().unwrap(), 0);
    let crypto = RustCrypto::default();
    let provider = Provider {
        crypto: &crypto,
        storage: SqliteStorageProvider::new(&fresh.connection),
    };
    let group = MlsGroup::load(provider.storage(), &GroupId::from_slice(&p.group))
        .unwrap()
        .unwrap();
    let member = group.members().next().unwrap();
    assert_eq!(group.members().count(), 1);
    assert_eq!(member.signature_key, p.device_key);
    assert_eq!(member.credential.serialized_content(), p.identity);
    let incoming = MlsMessageIn::tls_deserialize_exact(&p.group_info).unwrap();
    let MlsMessageBodyIn::GroupInfo(info) = incoming.extract() else {
        panic!("native public group info type")
    };
    assert_eq!(info.group_id().as_slice(), p.group);
    assert_eq!(info.epoch().as_u64(), 0);
    let mut verifier = TrustVerifier::from_native_pin(
        scope(),
        &p.authority,
        vec![NativeGroupFloor::from_native_store(&p.group, 0).unwrap()],
        NOW,
    )
    .unwrap();
    let roster = verifier.verify_roster(&p.signed_roster, NOW).unwrap();
    let approved = verifier
        .verify_device(&roster, &p.group, &p.identity, &p.device_key, NOW)
        .unwrap();
    assert_eq!(approved.account(), "admin-account");
    assert_eq!(approved.device(), "desktop");
    let (r, g): (Vec<u8>, Vec<u8>) = fresh
        .connection
        .query_row(
            "SELECT signed_roster,group_info FROM native_bootstrap_outbox WHERE id=1",
            [],
            |r| Ok((r.get(0)?, r.get(1)?)),
        )
        .unwrap();
    assert_eq!(r, p.signed_roster);
    assert_eq!(g, p.group_info);
    let bytes = std::fs::read(d.path().join("root.sqlite")).unwrap();
    assert!(!bytes.starts_with(b"SQLite format 3"));
}
#[test]
fn restored_root_pin_can_only_inspect_and_wrong_origin_group_or_authority_fail_closed() {
    let d = dir();
    let seeds = MemorySeeds::fresh();
    let path = d.path().join("root.sqlite");
    let fresh =
        FreshCommunity::provision_first_group(&path, &seeds, scope(), subject(), NOW).unwrap();
    let expected = pin(fresh.public_for_native_oob_ceremony());
    let wire = fresh.public_for_native_oob_ceremony().group_info.clone();
    let gid = fresh.public.group;
    drop(fresh);
    let inspect = RestoredInspection::load(&path, &seeds, expected.clone()).unwrap();
    assert_eq!(inspect.public_for_native_inspection().group_info, wire);
    assert_eq!(inspect.issue_or_send(), Err(Error::InspectionOnly));
    let other = ed25519_dalek::SigningKey::from_bytes(&[99; 32])
        .verifying_key()
        .to_bytes();
    for bad in [
        NativeRootPin::from_out_of_band(
            Scope::new("https://other.example", "fixture-community").unwrap(),
            &gid,
            expected.authority_key(),
        )
        .unwrap(),
        NativeRootPin::from_out_of_band(scope(), b"wrong-group", expected.authority_key()).unwrap(),
        NativeRootPin::from_out_of_band(scope(), &gid, other).unwrap(),
    ] {
        assert!(matches!(
            RestoredInspection::load(&path, &seeds, bad),
            Err(Error::Trust)
        ));
    }
    assert!(matches!(
        FreshCommunity::provision_first_group(&path, &seeds, scope(), subject(), NOW + 1),
        Err(Error::NativeUnavailable)
    ));
}
#[test]
fn native_subject_bounds_and_existing_aliases_cannot_reset_authority_floors() {
    for (a, d, i) in [
        ("", "desktop", b"id".as_slice()),
        ("admin", "wrong/device", b"id".as_slice()),
        ("admin", "desktop", b"".as_slice()),
    ] {
        assert!(NativeAdminSubject::from_native_admin_workflow(a, d, i).is_err());
    }
    let d = dir();
    let seeds = MemorySeeds::restored_public_fixture();
    assert!(matches!(
        FreshCommunity::provision_first_group(
            &d.path().join("new.sqlite"),
            &seeds,
            scope(),
            subject(),
            NOW
        ),
        Err(Error::NativeUnavailable)
    ));
    assert!(!d.path().join("new.sqlite").exists());
}
#[test]
fn fixture_child_first_group() {
    let Ok(path) = std::env::var("MNEMA_FIRST_GROUP_CHILD_PATH") else {
        return;
    };
    let seeds = MemorySeeds::fresh();
    let result =
        FreshCommunity::provision_first_group(Path::new(&path), &seeds, scope(), subject(), NOW);
    match std::env::var("MNEMA_FIRST_GROUP_FAULT").as_deref() {
        Ok(fault) if fault.starts_with("ignore-") => {
            assert!(matches!(result, Err(Error::Database)))
        }
        _ => panic!("child did not stop at expected native boundary"),
    }
}
#[test]
fn real_child_exits_and_suppressed_rows_leave_only_consistent_provider_outbox_or_orphan_inspection()
{
    for fault in [
        "before-commit",
        "after-commit",
        "ignore-record",
        "ignore-outbox",
    ] {
        let d = dir();
        let path = d.path().join("root.sqlite");
        let status = std::process::Command::new(std::env::current_exe().unwrap())
            .arg("--exact")
            .arg("tests::fixture_child_first_group")
            .arg("--test-threads=1")
            .env("MNEMA_FIRST_GROUP_CHILD_PATH", &path)
            .env("MNEMA_FIRST_GROUP_FAULT", fault)
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .status()
            .unwrap();
        assert_eq!(
            status.code(),
            Some(if fault.starts_with("ignore-") { 0 } else { 73 })
        );
        let seeds = MemorySeeds::restored_public_fixture();
        let c = open(&path, &seeds).unwrap();
        let gid: Vec<u8> = c
            .query_row("SELECT group_id FROM issuer_state WHERE id=1", [], |r| {
                r.get(0)
            })
            .unwrap();
        let count = |table: &str| {
            c.query_row(&format!("SELECT COUNT(*) FROM {table}"), [], |r| {
                r.get::<_, i64>(0)
            })
            .unwrap()
        };
        let complete = fault == "after-commit";
        assert_eq!(count("native_first_group"), i64::from(complete));
        assert_eq!(count("native_bootstrap_outbox"), i64::from(complete));
        let crypto = RustCrypto::default();
        let provider = Provider {
            crypto: &crypto,
            storage: SqliteStorageProvider::new(&c),
        };
        assert_eq!(
            MlsGroup::load(provider.storage(), &GroupId::from_slice(&gid))
                .unwrap()
                .is_some(),
            complete
        );
        let expected = NativeRootPin::from_out_of_band(
            scope(),
            &gid,
            ed25519_dalek::SigningKey::from_bytes(&MemorySeeds::seed("issuer-root"))
                .verifying_key()
                .to_bytes(),
        )
        .unwrap();
        if complete {
            let inspection = RestoredInspection::load(&path, &seeds, expected).unwrap();
            assert_eq!(inspection.issue_or_send(), Err(Error::InspectionOnly));
        } else {
            assert!(matches!(
                RestoredInspection::load(&path, &seeds, expected),
                Err(Error::Incomplete)
            ));
        }
        assert!(matches!(
            FreshCommunity::provision_first_group(&path, &seeds, scope(), subject(), NOW + 1),
            Err(Error::NativeUnavailable)
        ));
    }
}
#[cfg(target_os = "macos")]
#[test]
fn actual_owned_native_keychain_first_group_reopen_lock_and_explicit_fresh_recovery_root() {
    use crate::native_fixture::*;
    let _serial = serial().unwrap();
    let _no_ui = NoUi::begin().unwrap();
    let before = snapshot().unwrap();
    let d = dir();
    let r1 = d.path().join("first");
    let r2 = d.path().join("replacement");
    std::fs::create_dir(&r1).unwrap();
    std::fs::create_dir(&r2).unwrap();
    let mut first = OwnedFixture::create(&r1).unwrap();
    let path = r1.join("root.sqlite");
    assert!(first.path.starts_with(&r1));
    let fresh = FreshCommunity::provision_first_group(&path, &first.vault, scope(), subject(), NOW)
        .unwrap();
    let p = fresh.public_for_native_oob_ceremony();
    let expected = pin(p);
    let old_root = p.authority;
    let old_group = p.group;
    drop(fresh);
    let inspection = RestoredInspection::load(&path, &first.vault, expected.clone()).unwrap();
    assert_eq!(inspection.issue_or_send(), Err(Error::InspectionOnly));
    first.lock().unwrap();
    assert!(matches!(
        RestoredInspection::load(&path, &first.vault, expected.clone()),
        Err(Error::NativeUnavailable)
    ));
    first.unlock().unwrap();
    assert!(RestoredInspection::load(&path, &first.vault, expected.clone()).is_ok());
    let mut replacement = OwnedFixture::create(&r2).unwrap();
    let path2 = r2.join("root.sqlite");
    let reset = FreshCommunity::provision_first_group(
        &path2,
        &replacement.vault,
        scope(),
        subject(),
        NOW + 1,
    )
    .unwrap();
    let p = reset.public_for_native_oob_ceremony();
    assert_ne!(p.authority, old_root);
    assert_ne!(p.group, old_group);
    assert!(matches!(
        RestoredInspection::load(&path2, &replacement.vault, expected),
        Err(Error::Trust)
    ));
    drop(reset);
    replacement.delete().unwrap();
    first.delete().unwrap();
    assert!(before == snapshot().unwrap());
}

#[test]
fn actual_suppressed_provider_slots_signer_and_private_epoch_keys_never_return_fresh_owner() {
    for slot in [
        "join_group_config",
        "tree",
        "interim_transcript_hash",
        "context",
        "confirmation_tag",
        "group_state",
        "message_secrets",
        "resumption_psk_store",
        "own_leaf_index",
        "group_epoch_secrets",
        "signature",
        "epoch-keys",
    ] {
        let d = dir();
        let path = d.path().join("root.sqlite");
        let status = std::process::Command::new(std::env::current_exe().unwrap())
            .arg("--exact")
            .arg("tests::fixture_child_first_group")
            .arg("--test-threads=1")
            .env("MNEMA_FIRST_GROUP_CHILD_PATH", &path)
            .env("MNEMA_FIRST_GROUP_FAULT", format!("ignore-provider-{slot}"))
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .status()
            .unwrap();
        assert_eq!(
            status.code(),
            Some(0),
            "suppressed provider slot must fail closed"
        );
        let seeds = MemorySeeds::restored_public_fixture();
        let c = open(&path, &seeds).unwrap();
        for table in [
            "native_first_group",
            "native_bootstrap_outbox",
            "openmls_group_data",
            "openmls_epoch_keys_pairs",
            "openmls_signature_keys",
        ] {
            let count: i64 = c
                .query_row(&format!("SELECT COUNT(*) FROM {table}"), [], |r| r.get(0))
                .unwrap();
            assert_eq!(
                count, 0,
                "failed transaction must not publish partial state"
            );
        }
        assert_eq!(
            c.query_row("SELECT generation FROM issuer_state WHERE id=1", [], |r| {
                r.get::<_, i64>(0)
            })
            .unwrap(),
            1
        );
    }
}
