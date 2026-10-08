use super::*;
#[cfg(target_os = "macos")]
#[test]
fn actual_owned_keychain_generate_reopen_lock_fail_closed_unlock_delete_no_dialogs() {
    use crate::native::fixture::{NoUi, OwnedFixture, serial, snapshot};
    let _serial = serial().unwrap();
    use security_framework::os::macos::keychain::SecKeychain;
    let root = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("evidence/runs");
    std::fs::create_dir_all(&root).unwrap();
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(&root, std::fs::Permissions::from_mode(0o700)).unwrap();
    let dir = tempfile::Builder::new()
        .prefix("private-keychain-")
        .tempdir_in(&root)
        .unwrap();
    let before = snapshot().unwrap();
    let original = SecKeychain::user_interaction_allowed().unwrap();
    {
        let _no_ui = NoUi::begin().unwrap();
        assert!(!SecKeychain::user_interaction_allowed().unwrap());
        let mut fixture = OwnedFixture::create(dir.path()).unwrap();
        let root_public = fixture.vault.create_seed("authority-root").unwrap();
        let device_public = fixture.vault.create_seed("device-key").unwrap();
        let _ = fixture.vault.create_seed("database-key").unwrap();
        assert_ne!(root_public, device_public);
        assert_eq!(
            ed25519_dalek::SigningKey::from_bytes(
                &fixture.vault.read_seed("authority-root").unwrap()
            )
            .verifying_key()
            .to_bytes(),
            root_public
        );
        assert_eq!(
            fixture.vault.create_seed("authority-root"),
            Err(crate::Error::NativeUnavailable)
        );
        assert!(fixture.vault.read_seed("missing").is_err());
        fixture.lock().unwrap();
        assert!(matches!(
            fixture.vault.read_seed("device-key"),
            Err(crate::Error::NativeUnavailable)
        ));
        fixture.unlock().unwrap();
        assert_eq!(
            ed25519_dalek::SigningKey::from_bytes(&fixture.vault.read_seed("device-key").unwrap())
                .verifying_key()
                .to_bytes(),
            device_public
        );
        let service = fixture.vault.service_for_fixture();
        let reopened = NativeVault::from_owned_native_keychain(
            SecKeychain::open(&fixture.path).unwrap(),
            &service,
        )
        .unwrap();
        assert_eq!(
            *reopened.read_seed("database-key").unwrap(),
            *fixture.vault.read_seed("database-key").unwrap()
        );
        drop(reopened);
        fixture.delete().unwrap();
        assert!(fixture.vault.read_seed("authority-root").is_err());
        assert!(!fixture.path.exists());
    }
    assert_eq!(SecKeychain::user_interaction_allowed().unwrap(), original);
    let after = snapshot().unwrap();
    assert!(before == after);
    assert_eq!(std::fs::read_dir(dir.path()).unwrap().count(), 0);
}
use crate::pairing::{verify_invitation, verify_response};
use ed25519_dalek::SigningKey;
use mnema_crypto_adapter_candidate::Scope;
use mnema_crypto_trust_candidate::{NativeGroupFloor, TrustVerifier};
use std::{collections::HashMap, sync::Mutex};
const GROUP: &[u8] = b"enrollment-group";
const NOW: u64 = 1000;
struct MemoryFixture(Mutex<HashMap<String, Zeroizing<[u8; 32]>>>);
impl MemoryFixture {
    fn new() -> Self {
        Self(Mutex::new(HashMap::new()))
    }
}
impl NativeSecrets for MemoryFixture {
    fn read_seed(&self, alias: &str) -> Result<Zeroizing<[u8; 32]>> {
        self.0
            .lock()
            .unwrap()
            .get(alias)
            .cloned()
            .ok_or(Error::NativeUnavailable)
    }
    fn create_seed(&self, alias: &str) -> Result<[u8; 32]> {
        let mut store = self.0.lock().unwrap();
        if store.contains_key(alias) {
            return Err(crate::Error::NativeUnavailable);
        }
        let mut seed = Zeroizing::new([0; 32]);
        getrandom::fill(seed.as_mut()).map_err(|_| Error::Random)?;
        let public = SigningKey::from_bytes(&seed).verifying_key().to_bytes();
        store.insert(alias.into(), seed);
        Ok(public)
    }
}
fn scope() -> Scope {
    Scope::new("https://community.example", "community").unwrap()
}
fn intent(public: [u8; 32]) -> NativeAdmissionIntent {
    NativeAdmissionIntent::from_native_out_of_band_pin("bob", "desktop", b"bob-device", public)
        .unwrap()
}
fn dir() -> tempfile::TempDir {
    let root = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("evidence/runs");
    std::fs::create_dir_all(&root).unwrap();
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&root, std::fs::Permissions::from_mode(0o700)).unwrap();
    }
    tempfile::Builder::new()
        .prefix("private-issuer-")
        .tempdir_in(root)
        .unwrap()
}
fn trust(pin: &NativeRootPin, floor: u64) -> TrustVerifier {
    TrustVerifier::from_native_pin(
        pin.scope.clone(),
        &pin.authority,
        vec![NativeGroupFloor::from_native_store(&pin.group, floor).unwrap()],
        NOW,
    )
    .unwrap()
}
fn generation<S: NativeSecrets>(issuer: &Issuer<'_, S>) -> i64 {
    issuer
        .fixture_connection()
        .query_row("SELECT generation FROM issuer_state", [], |r| r.get(0))
        .unwrap()
}
fn used<S: NativeSecrets>(issuer: &Issuer<'_, S>) -> i64 {
    issuer
        .fixture_connection()
        .query_row("SELECT COALESCE(SUM(used),0) FROM invitations", [], |r| {
            r.get(0)
        })
        .unwrap()
}
#[test]
fn exact_device_possession_issues_frozen_profile_atomically_and_retries_identically() {
    let root = MemoryFixture::new();
    let device_keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&device_keys, pin.clone()).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let response = device.respond(&invitation, NOW).unwrap();
    assert_eq!(generation(&issuer), 0);
    let wire = issuer.admit_proven_device(&response, NOW).unwrap();
    assert_eq!(generation(&issuer), 1);
    assert_eq!(used(&issuer), 1);
    assert_eq!(issuer.admit_proven_device(&response, NOW).unwrap(), wire);
    assert_eq!(generation(&issuer), 1);
    let mut verifier = trust(&pin, 0);
    let roster = verifier.verify_roster(&wire, NOW).unwrap();
    let approved = verifier
        .verify_device(&roster, GROUP, b"bob-device", &device.public_key(), NOW)
        .unwrap();
    assert_eq!(approved.account(), "bob");
    assert_eq!(approved.device(), "desktop");
    let bound = verify_invitation(&pin, &invitation, NOW).unwrap();
    assert_eq!(
        issuer.inspect_issued_roster(&bound.nonce).unwrap(),
        Some(wire)
    );
}
#[test]
fn stolen_invitation_cannot_enroll_another_device_key() {
    let root = MemoryFixture::new();
    let first = MemoryFixture::new();
    let stolen = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&first, pin.clone()).unwrap();
    let other = Device::provision_native(&stolen, pin.clone()).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    assert!(matches!(
        other.respond(&invitation, NOW),
        Err(crate::Error::Trust)
    ));
    let response = device.respond(&invitation, NOW).unwrap();
    let mut spoofed = response.clone();
    let last = spoofed.proof.len() - 1;
    spoofed.proof[last] ^= 1;
    assert_eq!(
        issuer.admit_proven_device(&spoofed, NOW),
        Err(crate::Error::Signature)
    );
    assert_eq!(used(&issuer), 0);
    assert_eq!(generation(&issuer), 0);
    issuer.admit_proven_device(&response, NOW).unwrap();
}
#[test]
fn wrong_instance_root_group_tamper_and_expiry_fail_closed() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&keys, pin.clone()).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let mut wrong = pin.clone();
    wrong.scope = Scope::new("https://other.example", "community").unwrap();
    assert!(matches!(
        Device::reopen_native(&keys, wrong)
            .unwrap()
            .respond(&invitation, NOW),
        Err(crate::Error::Trust)
    ));
    let wrong =
        NativeRootPin::from_out_of_band(pin.scope.clone(), b"other-group", pin.authority).unwrap();
    assert!(matches!(
        Device::reopen_native(&keys, wrong)
            .unwrap()
            .respond(&invitation, NOW),
        Err(crate::Error::Trust)
    ));
    let other_root = SigningKey::from_bytes(&[5; 32]).verifying_key().to_bytes();
    let wrong = NativeRootPin::from_out_of_band(pin.scope.clone(), GROUP, other_root).unwrap();
    assert!(matches!(
        Device::reopen_native(&keys, wrong)
            .unwrap()
            .respond(&invitation, NOW),
        Err(crate::Error::Signature)
    ));
    assert!(matches!(
        device.respond(&invitation, NOW + 300),
        Err(crate::Error::Expired)
    ));
    assert!(matches!(
        device.respond(&invitation, NOW - 1),
        Err(crate::Error::Expired)
    ));
    let mut tampered = invitation.clone();
    *tampered.last_mut().unwrap() ^= 1;
    assert!(matches!(
        device.respond(&tampered, NOW),
        Err(crate::Error::Signature)
    ));
    assert_eq!(used(&issuer), 0);
}
#[test]
fn all_truncated_wire_prefixes_and_structural_bombs_reject_without_panics() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&keys, pin.clone()).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let response = device.respond(&invitation, NOW).unwrap();
    for end in 0..invitation.len() {
        assert!(device.respond(&invitation[..end], NOW).is_err());
    }
    for end in 0..response.proof.len() {
        let broken = ProofResponse {
            invitation: invitation.clone(),
            proof: response.proof[..end].to_vec(),
        };
        assert!(verify_response(&pin, &broken, NOW).is_err());
    }
    for bad in [
        vec![0x81; 1024],
        vec![0x9b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff],
        vec![0x9f, 0xff],
        vec![0; 8193],
    ] {
        assert!(device.respond(&bad, NOW).is_err());
    }
    assert_eq!(generation(&issuer), 0);
}
#[test]
fn actual_admission_outbox_abort_rolls_back_invitation_member_generation() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&keys, pin).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let response = device.respond(&invitation, NOW).unwrap();
    issuer.fixture_connection().execute_batch("CREATE TRIGGER fail_outbox BEFORE INSERT ON roster_outbox BEGIN SELECT RAISE(ABORT,'fixture');END;").unwrap();
    assert_eq!(
        issuer.admit_proven_device(&response, NOW),
        Err(crate::Error::Database)
    );
    assert_eq!(used(&issuer), 0);
    assert_eq!(generation(&issuer), 0);
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT COUNT(*) FROM members", [], |r| r.get::<_, i64>(0))
            .unwrap(),
        0
    );
    assert_eq!(
        issuer.admit_proven_device(&response, NOW),
        Err(crate::Error::Database)
    );
}
#[test]
fn restored_authority_never_issues_and_existing_root_cannot_reset_floor() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let path = dir.path().join("issuer.sqlite");
    let mut issuer = Issuer::provision_fresh(&path, &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&keys, pin.clone()).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let response = device.respond(&invitation, NOW).unwrap();
    let wire = issuer.admit_proven_device(&response, NOW).unwrap();
    let nonce = verify_invitation(&pin, &invitation, NOW).unwrap().nonce;
    drop(issuer);
    let mut restored = Issuer::restore_inspection_only(&path, &root, pin.clone()).unwrap();
    assert_eq!(
        restored.invite_native_reviewed_device(intent(device.public_key()), NOW),
        Err(crate::Error::Restored)
    );
    assert_eq!(
        restored.admit_proven_device(&response, NOW),
        Err(crate::Error::Restored)
    );
    assert_eq!(
        restored.revoke_exact_native_device(intent(device.public_key()), NOW),
        Err(crate::Error::Restored)
    );
    assert_eq!(restored.inspect_issued_roster(&nonce).unwrap(), Some(wire));
    assert!(matches!(
        Issuer::provision_fresh(&dir.path().join("reset.sqlite"), &root, scope(), GROUP),
        Err(crate::Error::NativeUnavailable)
    ));
}
#[test]
fn exact_native_revocation_issues_new_roster_and_old_generation_is_stale() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&keys, pin.clone()).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let response = device.respond(&invitation, NOW).unwrap();
    let first = issuer.admit_proven_device(&response, NOW).unwrap();
    let mut verifier = trust(&pin, 0);
    let old = verifier.verify_roster(&first, NOW).unwrap();
    let revoked = issuer
        .revoke_exact_native_device(intent(device.public_key()), NOW + 1)
        .unwrap();
    verifier.verify_roster(&revoked, NOW + 1).unwrap();
    assert!(
        verifier
            .verify_device(&old, GROUP, b"bob-device", &device.public_key(), NOW + 1)
            .is_err()
    );
    assert!(verifier.verify_roster(&first, NOW + 1).is_err());
    assert_eq!(generation(&issuer), 2);
    assert_eq!(
        issuer.admit_proven_device(&response, NOW + 1),
        Err(crate::Error::Replay)
    );
    assert_eq!(generation(&issuer), 2);
}
#[cfg(target_os = "macos")]
#[test]
fn actual_separate_native_root_device_keychains_issue_roster_and_validate_same_mls_key() {
    use crate::native::fixture::{NoUi, OwnedFixture, serial, snapshot};
    use openmls::prelude::{
        tls_codec::{Deserialize, Serialize},
        *,
    };
    use openmls_basic_credential::SignatureKeyPair;
    use openmls_rust_crypto::OpenMlsRustCrypto;
    use openmls_traits::OpenMlsProvider;
    use security_framework::os::macos::keychain::SecKeychain;
    let _serial = serial().unwrap();
    let before = snapshot().unwrap();
    let original = SecKeychain::user_interaction_allowed().unwrap();
    let root_dir = dir();
    let device_dir = dir();
    {
        let _no_ui = NoUi::begin().unwrap();
        let mut root = OwnedFixture::create(root_dir.path()).unwrap();
        let mut client = OwnedFixture::create(device_dir.path()).unwrap();
        let mut issuer = Issuer::provision_fresh(
            &root_dir.path().join("issuer.sqlite"),
            &root.vault,
            scope(),
            GROUP,
        )
        .unwrap();
        let pin = issuer.pin_for_native_out_of_band_transfer();
        let device = Device::provision_native(&client.vault, pin.clone()).unwrap();
        let invite = issuer
            .invite_native_reviewed_device(intent(device.public_key()), NOW)
            .unwrap();
        let pending_invite = issuer
            .invite_native_reviewed_device(intent(device.public_key()), NOW)
            .unwrap();
        client.lock().unwrap();
        assert!(matches!(
            device.respond(&invite, NOW),
            Err(crate::Error::NativeUnavailable)
        ));
        client.unlock().unwrap();
        let response = device.respond(&invite, NOW).unwrap();
        root.lock().unwrap();
        assert_eq!(
            issuer.admit_proven_device(&response, NOW),
            Err(crate::Error::NativeUnavailable)
        );
        root.unlock().unwrap();
        let wire = issuer.admit_proven_device(&response, NOW).unwrap();
        let mut verifier = trust(&pin, 0);
        let roster = verifier.verify_roster(&wire, NOW).unwrap();
        verifier
            .verify_device(&roster, GROUP, b"bob-device", &device.public_key(), NOW)
            .unwrap();
        let seed = client.vault.read_seed("device-key").unwrap();
        let signer = SignatureKeyPair::from_raw(
            SignatureScheme::ED25519,
            seed.to_vec(),
            device.public_key().to_vec(),
        );
        let provider = OpenMlsRustCrypto::default();
        let bundle = KeyPackage::builder()
            .build(
                Ciphersuite::MLS_128_DHKEMX25519_CHACHA20POLY1305_SHA256_Ed25519,
                &provider,
                &signer,
                CredentialWithKey {
                    credential: BasicCredential::new(b"bob-device".to_vec()).into(),
                    signature_key: device.public_key().to_vec().into(),
                },
            )
            .unwrap();
        let package_wire = bundle.key_package().tls_serialize_detached().unwrap();
        let parsed = KeyPackageIn::tls_deserialize_exact(&package_wire).unwrap();
        let valid = parsed
            .validate(provider.crypto(), ProtocolVersion::Mls10)
            .unwrap();
        assert_eq!(
            valid.leaf_node().signature_key().as_slice(),
            device.public_key()
        );
        verifier
            .verify_device(
                &roster,
                GROUP,
                valid.leaf_node().credential().serialized_content(),
                valid.leaf_node().signature_key().as_slice(),
                NOW,
            )
            .unwrap();
        let pending_response = device.respond(&pending_invite, NOW).unwrap();
        let revoked = issuer
            .revoke_exact_native_device(intent(device.public_key()), NOW + 1)
            .unwrap();
        verifier.verify_roster(&revoked, NOW + 1).unwrap();
        assert_eq!(
            issuer.admit_proven_device(&pending_response, NOW + 1),
            Err(crate::Error::Replay)
        );
        assert_eq!(generation(&issuer), 2);
        assert_eq!(
            issuer
                .fixture_connection()
                .query_row("SELECT COUNT(*) FROM members", [], |r| r.get::<_, i64>(0))
                .unwrap(),
            0
        );
        assert_eq!(
            issuer
                .fixture_connection()
                .query_row("SELECT COUNT(*) FROM roster_outbox", [], |r| r
                    .get::<_, i64>(0))
                .unwrap(),
            2
        );
        drop(device);
        drop(issuer);
        root.delete().unwrap();
        client.delete().unwrap();
        assert!(root.vault.read_seed("issuer-root").is_err());
        assert!(client.vault.read_seed("device-key").is_err());
    }
    assert_eq!(SecKeychain::user_interaction_allowed().unwrap(), original);
    assert!(before == snapshot().unwrap());
}
#[test]
fn root_signed_but_unrecorded_invitation_is_not_native_admission() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&keys, pin.clone()).unwrap();
    let root_signer = SigningKey::from_bytes(&root.read_seed("issuer-root").unwrap());
    let invitation = crate::pairing::make_invitation(
        &pin,
        &intent(device.public_key()),
        [9; 32],
        NOW,
        &root_signer,
    )
    .unwrap();
    let response = device.respond(&invitation, NOW).unwrap();
    assert_eq!(
        issuer.admit_proven_device(&response, NOW),
        Err(crate::Error::Replay)
    );
    assert_eq!(generation(&issuer), 0);
}
#[test]
fn actual_deferred_commit_failure_rolls_back_signed_generation_and_nonce_use() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&keys, pin.clone()).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let response = device.respond(&invitation, NOW).unwrap();
    let nonce = verify_invitation(&pin, &invitation, NOW).unwrap().nonce;
    issuer.fixture_connection().execute_batch("CREATE TABLE fixture_parent(id INTEGER PRIMARY KEY);CREATE TABLE fixture_child(id INTEGER REFERENCES fixture_parent(id) DEFERRABLE INITIALLY DEFERRED);CREATE TRIGGER fail_commit AFTER INSERT ON roster_outbox BEGIN INSERT INTO fixture_child VALUES(1);END;").unwrap();
    assert_eq!(
        issuer.admit_proven_device(&response, NOW),
        Err(crate::Error::Database)
    );
    assert_eq!(used(&issuer), 0);
    assert_eq!(generation(&issuer), 0);
    assert_eq!(issuer.inspect_issued_roster(&nonce).unwrap(), None);
}
#[test]
fn changed_native_root_and_backward_issuer_clock_fail_closed() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&keys, pin).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW + 1)
        .unwrap();
    let response = device.respond(&invitation, NOW + 1).unwrap();
    assert_eq!(
        issuer.admit_proven_device(&response, NOW),
        Err(crate::Error::Expired)
    );
    assert_eq!(
        issuer.invite_native_reviewed_device(intent(device.public_key()), NOW),
        Err(crate::Error::Replay)
    );
    root.0
        .lock()
        .unwrap()
        .insert("issuer-root".into(), Zeroizing::new([5; 32]));
    assert_eq!(
        issuer.admit_proven_device(&response, NOW + 1),
        Err(crate::Error::Trust)
    );
    assert_eq!(generation(&issuer), 0);
    assert_eq!(used(&issuer), 0);
}
#[cfg(target_os = "macos")]
#[test]
fn actual_native_child_admission_exit_helper() {
    let Ok(database) = std::env::var("MNEMA_ENROLLMENT_CHILD_DATABASE") else {
        return;
    };
    use crate::native::fixture::NoUi;
    use security_framework::os::macos::keychain::SecKeychain;
    let fixture_root = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("evidence/runs");
    assert!(std::path::Path::new(&database).starts_with(&fixture_root));
    let path = std::env::var("MNEMA_ENROLLMENT_CHILD_KEYCHAIN").unwrap();
    assert!(std::path::Path::new(&path).starts_with(&fixture_root));
    let service = std::env::var("MNEMA_ENROLLMENT_CHILD_SERVICE").unwrap();
    let _no_ui = NoUi::begin().unwrap();
    let vault =
        NativeVault::from_owned_native_keychain(SecKeychain::open(&path).unwrap(), &service)
            .unwrap();
    let mut issuer =
        Issuer::provision_fresh(std::path::Path::new(&database), &vault, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&vault, pin).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let response = device.respond(&invitation, NOW).unwrap();
    let _ = issuer.admit_proven_device(&response, NOW);
    panic!("fixture exit not reached");
}
#[cfg(target_os = "macos")]
#[test]
fn three_actual_native_keychain_sqlcipher_admission_process_exits_preserve_atomicity() {
    use crate::native::fixture::{NoUi, OwnedFixture, serial, snapshot};
    use security_framework::os::macos::keychain::SecKeychain;
    let _serial = serial().unwrap();
    let before = snapshot().unwrap();
    let original = SecKeychain::user_interaction_allowed().unwrap();
    {
        let _no_ui = NoUi::begin().unwrap();
        for point in ["after-roster", "after-outbox", "after-commit"] {
            let dir = dir();
            let mut fixture = OwnedFixture::create(dir.path()).unwrap();
            let database = dir.path().join("issuer.sqlite");
            let status = std::process::Command::new(std::env::current_exe().unwrap())
                .args([
                    "--exact",
                    "tests::actual_native_child_admission_exit_helper",
                    "--nocapture",
                ])
                .env("MNEMA_ENROLLMENT_CHILD_DATABASE", &database)
                .env("MNEMA_ENROLLMENT_CHILD_KEYCHAIN", &fixture.path)
                .env(
                    "MNEMA_ENROLLMENT_CHILD_SERVICE",
                    fixture.vault.service_for_fixture(),
                )
                .env("MNEMA_ENROLLMENT_FAULT", point)
                .status()
                .unwrap();
            assert_eq!(status.code(), Some(73));
            let root_public =
                SigningKey::from_bytes(&fixture.vault.read_seed("issuer-root").unwrap())
                    .verifying_key()
                    .to_bytes();
            let pin = NativeRootPin::from_out_of_band(scope(), GROUP, root_public).unwrap();
            let mut restored =
                Issuer::restore_inspection_only(&database, &fixture.vault, pin.clone()).unwrap();
            let device_public =
                SigningKey::from_bytes(&fixture.vault.read_seed("device-key").unwrap())
                    .verifying_key()
                    .to_bytes();
            assert_eq!(
                restored.invite_native_reviewed_device(intent(device_public), NOW),
                Err(crate::Error::Restored)
            );
            let expected = if point == "after-commit" { 1 } else { 0 };
            assert_eq!(generation(&restored), expected);
            assert_eq!(used(&restored), expected);
            let members: i64 = restored
                .fixture_connection()
                .query_row("SELECT COUNT(*) FROM members", [], |r| r.get(0))
                .unwrap();
            let outbox: i64 = restored
                .fixture_connection()
                .query_row("SELECT COUNT(*) FROM roster_outbox", [], |r| r.get(0))
                .unwrap();
            assert_eq!((members, outbox), (expected, expected));
            if expected == 1 {
                let wire: Vec<u8> = restored
                    .fixture_connection()
                    .query_row("SELECT wire FROM roster_outbox", [], |r| r.get(0))
                    .unwrap();
                let mut verifier = trust(&pin, 0);
                let roster = verifier.verify_roster(&wire, NOW).unwrap();
                verifier
                    .verify_device(&roster, GROUP, b"bob-device", &device_public, NOW)
                    .unwrap();
            }
            drop(restored);
            fixture.delete().unwrap();
            assert!(fixture.vault.read_seed("issuer-root").is_err());
        }
    }
    assert!(before == snapshot().unwrap());
    assert_eq!(SecKeychain::user_interaction_allowed().unwrap(), original);
}
#[test]
fn two_signed_pending_invitations_cannot_re_admit_after_exact_native_revocation() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&keys, pin.clone()).unwrap();
    let first = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let second = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let first_response = device.respond(&first, NOW).unwrap();
    let second_response = device.respond(&second, NOW).unwrap();
    issuer.admit_proven_device(&first_response, NOW).unwrap();
    issuer
        .revoke_exact_native_device(intent(device.public_key()), NOW + 1)
        .unwrap();
    let outbox: i64 = issuer
        .fixture_connection()
        .query_row("SELECT COUNT(*) FROM roster_outbox", [], |r| r.get(0))
        .unwrap();
    assert_eq!(outbox, 2);
    assert_eq!(generation(&issuer), 2);
    assert_eq!(
        issuer.admit_proven_device(&second_response, NOW + 1),
        Err(crate::Error::Replay)
    );
    assert_eq!(generation(&issuer), 2);
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT COUNT(*) FROM members", [], |r| r.get::<_, i64>(0))
            .unwrap(),
        0
    );
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT COUNT(*) FROM roster_outbox", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        outbox
    );
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row(
                "SELECT COUNT(*) FROM invitations WHERE used=0 AND revoked=1",
                [],
                |r| r.get::<_, i64>(0)
            )
            .unwrap(),
        1
    );
}
#[test]
fn revocation_outbox_failure_rolls_back_pending_invitation_invalidation() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&keys, pin).unwrap();
    let first = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let second = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    issuer
        .admit_proven_device(&device.respond(&first, NOW).unwrap(), NOW)
        .unwrap();
    let _ = device.respond(&second, NOW).unwrap();
    issuer.fixture_connection().execute_batch("CREATE TRIGGER fail_revoke BEFORE INSERT ON roster_outbox BEGIN SELECT RAISE(ABORT,'fixture');END;").unwrap();
    assert_eq!(
        issuer.revoke_exact_native_device(intent(device.public_key()), NOW + 1),
        Err(crate::Error::Database)
    );
    assert_eq!(generation(&issuer), 1);
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT COUNT(*) FROM members", [], |r| r.get::<_, i64>(0))
            .unwrap(),
        1
    );
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT SUM(revoked) FROM invitations", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        0
    );
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT COUNT(*) FROM roster_outbox", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        1
    );
}
#[test]
fn suppressed_generation_write_never_publishes_a_roster_without_its_floor() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&keys, pin).unwrap();
    let invite = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let response = device.respond(&invite, NOW).unwrap();
    issuer.fixture_connection().execute_batch("CREATE TRIGGER suppress_generation BEFORE UPDATE OF generation ON issuer_state BEGIN SELECT RAISE(IGNORE);END;").unwrap();
    assert_eq!(
        issuer.admit_proven_device(&response, NOW),
        Err(crate::Error::Database)
    );
    assert_eq!(generation(&issuer), 0);
    assert_eq!(used(&issuer), 0);
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT COUNT(*) FROM members", [], |r| r.get::<_, i64>(0))
            .unwrap(),
        0
    );
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT COUNT(*) FROM roster_outbox", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        0
    );
}
#[test]
fn suppressed_invitation_revocation_rolls_back_device_removal() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let pin = issuer.pin_for_native_out_of_band_transfer();
    let device = Device::provision_native(&keys, pin).unwrap();
    let first = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let _ = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    issuer
        .admit_proven_device(&device.respond(&first, NOW).unwrap(), NOW)
        .unwrap();
    issuer.fixture_connection().execute_batch("CREATE TRIGGER suppress_revoke BEFORE UPDATE OF revoked ON invitations BEGIN SELECT RAISE(IGNORE);END;").unwrap();
    assert_eq!(
        issuer.revoke_exact_native_device(intent(device.public_key()), NOW + 1),
        Err(crate::Error::Database)
    );
    assert_eq!(generation(&issuer), 1);
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT COUNT(*) FROM members", [], |r| r.get::<_, i64>(0))
            .unwrap(),
        1
    );
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT SUM(revoked) FROM invitations", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        0
    );
}

#[test]
fn suppressed_critical_inserts_never_return_or_commit_issued_authorization() {
    for table in ["invitations", "members", "roster_outbox"] {
        let root = MemoryFixture::new();
        let keys = MemoryFixture::new();
        let dir = dir();
        let mut issuer =
            Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP)
                .unwrap();
        let device =
            Device::provision_native(&keys, issuer.pin_for_native_out_of_band_transfer()).unwrap();
        let invitation = if table == "invitations" {
            None
        } else {
            Some(
                issuer
                    .invite_native_reviewed_device(intent(device.public_key()), NOW)
                    .unwrap(),
            )
        };
        issuer.fixture_connection().execute_batch(&format!("CREATE TRIGGER suppress_insert BEFORE INSERT ON {table} BEGIN SELECT RAISE(IGNORE);END;")).unwrap();
        let result = match invitation {
            None => issuer.invite_native_reviewed_device(intent(device.public_key()), NOW),
            Some(wire) => issuer.admit_proven_device(&device.respond(&wire, NOW).unwrap(), NOW),
        };
        assert_eq!(result, Err(crate::Error::Database), "{table}");
        assert_eq!(generation(&issuer), 0);
        assert_eq!(used(&issuer), 0);
        for unchanged in ["members", "roster_outbox"] {
            assert_eq!(
                issuer
                    .fixture_connection()
                    .query_row(&format!("SELECT COUNT(*) FROM {unchanged}"), [], |r| r
                        .get::<_, i64>(0))
                    .unwrap(),
                0
            );
        }
        assert_eq!(
            issuer.invite_native_reviewed_device(intent(device.public_key()), NOW),
            Err(crate::Error::Database)
        );
    }
}

#[test]
fn suppressed_revocation_outbox_rolls_back_exact_device_and_pending_invitation() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let device =
        Device::provision_native(&keys, issuer.pin_for_native_out_of_band_transfer()).unwrap();
    let first = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    issuer
        .admit_proven_device(&device.respond(&first, NOW).unwrap(), NOW)
        .unwrap();
    issuer.fixture_connection().execute_batch("CREATE TRIGGER suppress_outbox BEFORE INSERT ON roster_outbox BEGIN SELECT RAISE(IGNORE);END;").unwrap();
    assert_eq!(
        issuer.revoke_exact_native_device(intent(device.public_key()), NOW + 1),
        Err(crate::Error::Database)
    );
    assert_eq!(generation(&issuer), 1);
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT COUNT(*) FROM members", [], |r| r.get::<_, i64>(0))
            .unwrap(),
        1
    );
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT COUNT(*) FROM roster_outbox", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        1
    );
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT SUM(revoked) FROM invitations", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        0
    );
}

#[test]
fn already_admitted_device_second_pending_proof_does_not_quarantine_authority() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let device =
        Device::provision_native(&keys, issuer.pin_for_native_out_of_band_transfer()).unwrap();
    let first = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let second = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    issuer
        .admit_proven_device(&device.respond(&first, NOW).unwrap(), NOW)
        .unwrap();
    assert_eq!(
        issuer.admit_proven_device(&device.respond(&second, NOW).unwrap(), NOW),
        Err(crate::Error::Replay)
    );
    assert_eq!(generation(&issuer), 1);
    assert_eq!(used(&issuer), 1);
    assert!(
        issuer
            .revoke_exact_native_device(intent(device.public_key()), NOW + 1)
            .is_ok()
    );
    assert_eq!(generation(&issuer), 2);
}

#[test]
fn successful_exact_admission_retry_advances_native_clock_floor() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let device =
        Device::provision_native(&keys, issuer.pin_for_native_out_of_band_transfer()).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let response = device.respond(&invitation, NOW).unwrap();
    let first = issuer.admit_proven_device(&response, NOW).unwrap();
    assert_eq!(
        issuer.admit_proven_device(&response, NOW + 200).unwrap(),
        first
    );
    assert_eq!(
        issuer.invite_native_reviewed_device(intent(device.public_key()), NOW + 100),
        Err(crate::Error::Replay)
    );
    assert_eq!(generation(&issuer), 1);
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT COUNT(*) FROM roster_outbox", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        1
    );
    issuer.fixture_connection().execute_batch("CREATE TRIGGER suppress_time BEFORE UPDATE OF observed_time ON issuer_state BEGIN SELECT RAISE(IGNORE);END;").unwrap();
    assert_eq!(
        issuer.admit_proven_device(&response, NOW + 201),
        Err(crate::Error::Database)
    );
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT observed_time FROM issuer_state", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        (NOW + 200) as i64
    );
}

#[test]
fn suppressed_nonce_consumption_is_database_failure_and_permanently_quarantines() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let device =
        Device::provision_native(&keys, issuer.pin_for_native_out_of_band_transfer()).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    let response = device.respond(&invitation, NOW).unwrap();
    issuer.fixture_connection().execute_batch("CREATE TRIGGER suppress_used BEFORE UPDATE OF used ON invitations BEGIN SELECT RAISE(IGNORE);END;").unwrap();
    assert_eq!(
        issuer.admit_proven_device(&response, NOW),
        Err(crate::Error::Database)
    );
    assert_eq!(generation(&issuer), 0);
    assert_eq!(used(&issuer), 0);
    issuer
        .fixture_connection()
        .execute_batch("DROP TRIGGER suppress_used;")
        .unwrap();
    assert_eq!(
        issuer.admit_proven_device(&response, NOW),
        Err(crate::Error::Database)
    );
    assert_eq!(generation(&issuer), 0);
}

#[test]
fn suppressed_existing_device_delete_is_database_failure_and_quarantines() {
    let root = MemoryFixture::new();
    let keys = MemoryFixture::new();
    let dir = dir();
    let mut issuer =
        Issuer::provision_fresh(&dir.path().join("issuer.sqlite"), &root, scope(), GROUP).unwrap();
    let device =
        Device::provision_native(&keys, issuer.pin_for_native_out_of_band_transfer()).unwrap();
    let invitation = issuer
        .invite_native_reviewed_device(intent(device.public_key()), NOW)
        .unwrap();
    issuer
        .admit_proven_device(&device.respond(&invitation, NOW).unwrap(), NOW)
        .unwrap();
    issuer.fixture_connection().execute_batch("CREATE TRIGGER suppress_delete BEFORE DELETE ON members BEGIN SELECT RAISE(IGNORE);END;").unwrap();
    assert_eq!(
        issuer.revoke_exact_native_device(intent(device.public_key()), NOW + 1),
        Err(crate::Error::Database)
    );
    assert_eq!(generation(&issuer), 1);
    issuer
        .fixture_connection()
        .execute_batch("DROP TRIGGER suppress_delete;")
        .unwrap();
    assert_eq!(
        issuer.revoke_exact_native_device(intent(device.public_key()), NOW + 1),
        Err(crate::Error::Database)
    );
    assert_eq!(
        issuer
            .fixture_connection()
            .query_row("SELECT COUNT(*) FROM members", [], |r| r.get::<_, i64>(0))
            .unwrap(),
        1
    );
}
