use super::*;
use openmls_basic_credential::SignatureKeyPair;
use openmls_traits::OpenMlsProvider;

fn identity(
    provider: &OpenMlsRustCrypto,
    identity: &[u8],
) -> (CredentialWithKey, SignatureKeyPair) {
    let signer = SignatureKeyPair::new(SignatureScheme::ED25519).unwrap();
    signer.store(provider.storage()).unwrap();
    (
        CredentialWithKey {
            credential: BasicCredential::new(identity.to_vec()).into(),
            signature_key: signer.to_public_vec().into(),
        },
        signer,
    )
}
fn record(account: &str, device: &str, credential: &CredentialWithKey) -> NativeApprovalRecord {
    NativeApprovalRecord::from_native_store(
        account,
        device,
        credential.credential.serialized_content(),
        credential.signature_key.as_slice(),
    )
    .unwrap()
}
struct Fixture {
    adapter: Adapter,
    handle: GroupHandle,
    bob_provider: OpenMlsRustCrypto,
    bob_group: MlsGroup,
    bob_signer: SignatureKeyPair,
    charlie_package: KeyPackageBundle,
    records: Vec<NativeApprovalRecord>,
    alice_index: LeafNodeIndex,
}
impl Fixture {
    fn new(third_approved: bool, spoof: bool) -> Self {
        let scope = Scope::new("https://community.example/", "community-test").unwrap();
        let alice_provider = OpenMlsRustCrypto::default();
        let bob_provider = OpenMlsRustCrypto::default();
        let charlie_provider = OpenMlsRustCrypto::default();
        let (alice_credential, alice_signer) = identity(&alice_provider, b"alice-desktop");
        let (bob_credential, bob_signer) = identity(&bob_provider, b"bob-browser");
        let (charlie_credential, charlie_signer) = identity(
            &charlie_provider,
            if spoof {
                b"alice-desktop"
            } else {
                b"charlie-desktop"
            },
        );
        let mut records = vec![
            record("alice", "desktop", &alice_credential),
            record("bob", "browser", &bob_credential),
        ];
        if third_approved {
            records.push(record("charlie", "desktop", &charlie_credential));
        }
        let config = MlsGroupCreateConfig::builder()
            .ciphersuite(SUITE)
            .use_ratchet_tree_extension(true)
            .build();
        let mut alice_group =
            MlsGroup::new(&alice_provider, &alice_signer, &config, alice_credential).unwrap();
        let alice_index = alice_group.own_leaf_index();
        let bob_package = KeyPackage::builder()
            .build(SUITE, &bob_provider, &bob_signer, bob_credential)
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
            panic!("welcome fixture");
        };
        let bob_group =
            StagedWelcome::new_from_welcome(&bob_provider, config.join_config(), welcome, None)
                .unwrap()
                .into_group(&bob_provider)
                .unwrap();
        let charlie_package = KeyPackage::builder()
            .build(
                SUITE,
                &charlie_provider,
                &charlie_signer,
                charlie_credential,
            )
            .unwrap();
        let policy = NativeTrustPolicy::from_native_store(scope.clone(), records.clone()).unwrap();
        let mut adapter = Adapter::from_native_state(scope, policy, alice_provider).unwrap();
        let handle = adapter.adopt_native_group(alice_group).unwrap();
        Self {
            adapter,
            handle,
            bob_provider,
            bob_group,
            bob_signer,
            charlie_package,
            records,
            alice_index,
        }
    }
    fn add_commit(&mut self) -> Vec<u8> {
        let (commit, _, _) = self
            .bob_group
            .add_members(
                &self.bob_provider,
                &self.bob_signer,
                std::slice::from_ref(self.charlie_package.key_package()),
            )
            .unwrap();
        commit.to_bytes().unwrap()
    }
    fn application(&mut self, content: &[u8]) -> Vec<u8> {
        self.bob_group
            .create_message(&self.bob_provider, &self.bob_signer, content)
            .unwrap()
            .to_bytes()
            .unwrap()
    }
}

#[test]
fn approved_commit_requires_inspection_and_explicit_authorization_before_merge() {
    let mut f = Fixture::new(true, false);
    let before = f.adapter.summary(f.handle).unwrap();
    let wire = f.add_commit();
    let stage = f.adapter.stage_commit(f.handle, &wire).unwrap();
    assert_eq!(f.adapter.summary(f.handle).unwrap(), before);
    assert_eq!(f.adapter.authorize_stage(stage), Err(Error::NotInspected));
    let inspection = f.adapter.inspect_stage(stage).unwrap();
    assert_eq!(inspection.additions.len(), 1);
    assert_eq!(inspection.additions[0].identity, b"charlie-desktop");
    assert_eq!(inspection.from_epoch, before.epoch);
    let authorized = f.adapter.authorize_stage(stage).unwrap();
    assert_eq!(f.adapter.summary(f.handle).unwrap(), before);
    let merged = f.adapter.merge_authorized(authorized).unwrap();
    assert_eq!(merged.members, 3);
    assert_eq!(merged.epoch, before.epoch + 1);
    assert_eq!(
        f.adapter.merge_authorized(authorized),
        Err(Error::StaleHandle)
    );
    assert_eq!(
        f.adapter.stage_commit(f.handle, &wire),
        Err(Error::ProtocolRejected)
    );
}

#[test]
fn spoofed_identity_key_is_rejected_before_merge_and_existing_state_is_preserved() {
    let mut f = Fixture::new(false, true);
    let before = f.adapter.summary(f.handle).unwrap();
    let original_tree = f
        .adapter
        .groups
        .get(&f.handle)
        .unwrap()
        .export_ratchet_tree();
    let wire = f.add_commit();
    let stage = f.adapter.stage_commit(f.handle, &wire).unwrap();
    let inspection = f.adapter.inspect_stage(stage).unwrap();
    assert_eq!(inspection.additions[0].identity, b"alice-desktop");
    assert_eq!(
        f.adapter.authorize_stage(stage),
        Err(Error::UnapprovedDevice)
    );
    assert_eq!(
        f.adapter.merge_authorized(AuthorizedStageHandle { stage }),
        Err(Error::NotAuthorized)
    );
    f.adapter.reject_stage(stage).unwrap();
    assert_eq!(f.adapter.summary(f.handle).unwrap(), before);
    assert_eq!(
        f.adapter
            .groups
            .get(&f.handle)
            .unwrap()
            .export_ratchet_tree(),
        original_tree
    );
    assert_eq!(f.adapter.inspect_stage(stage), Err(Error::StaleHandle));
}

#[test]
fn unknown_device_is_rejected_even_with_valid_member_signature() {
    let mut f = Fixture::new(false, false);
    let wire = f.add_commit();
    let stage = f.adapter.stage_commit(f.handle, &wire).unwrap();
    f.adapter.inspect_stage(stage).unwrap();
    assert_eq!(
        f.adapter.authorize_stage(stage),
        Err(Error::UnapprovedDevice)
    );
}

#[test]
fn ordinary_self_update_path_requires_same_approved_identity_and_key() {
    let mut f = Fixture::new(false, false);
    let (commit, _, _) = f
        .bob_group
        .self_update(
            &f.bob_provider,
            &f.bob_signer,
            LeafNodeParameters::default(),
        )
        .unwrap()
        .into_contents();
    let stage = f
        .adapter
        .stage_commit(f.handle, &commit.to_bytes().unwrap())
        .unwrap();
    let inspection = f.adapter.inspect_stage(stage).unwrap();
    assert_eq!(
        inspection.update_path.as_ref().unwrap().identity,
        b"bob-browser"
    );
    let authorized = f.adapter.authorize_stage(stage).unwrap();
    assert_eq!(f.adapter.merge_authorized(authorized).unwrap().members, 2);
}

#[test]
fn valid_context_extension_change_is_staged_but_not_authorized() {
    let mut f = Fixture::new(false, false);
    // Fixture member deliberately constructs an inline context-change commit.
    // Acceptance by the malicious sender does not grant adapter authorization.
    let (commit, _, _) = f
        .bob_group
        .commit_builder()
        .propose_group_context_extensions(Extensions::default())
        .unwrap()
        .load_psks(f.bob_provider.storage())
        .unwrap()
        .build(
            f.bob_provider.rand(),
            f.bob_provider.crypto(),
            &f.bob_signer,
            |_| true,
        )
        .unwrap()
        .stage_commit(&f.bob_provider)
        .unwrap()
        .into_contents();
    let before = f.adapter.summary(f.handle).unwrap();
    let stage = f
        .adapter
        .stage_commit(f.handle, &commit.to_bytes().unwrap())
        .unwrap();
    f.adapter.inspect_stage(stage).unwrap();
    assert_eq!(
        f.adapter.authorize_stage(stage),
        Err(Error::UnsupportedPolicyChange)
    );
    assert_eq!(f.adapter.summary(f.handle).unwrap(), before);
}

#[test]
fn provider_public_self_update_does_not_make_application_content_public() {
    let mut f = Fixture::new(false, false);
    let config = MlsGroupJoinConfig::builder()
        .wire_format_policy(MIXED_PLAINTEXT_WIRE_FORMAT_POLICY)
        .use_ratchet_tree_extension(true)
        .build();
    f.bob_group
        .set_configuration(f.bob_provider.storage(), &config)
        .unwrap();
    let (commit, _, _) = f
        .bob_group
        .self_update(
            &f.bob_provider,
            &f.bob_signer,
            LeafNodeParameters::default(),
        )
        .unwrap()
        .into_contents();
    let wire = commit.to_bytes().unwrap();
    assert!(matches!(
        MlsMessageIn::tls_deserialize_exact(&wire)
            .unwrap()
            .extract(),
        MlsMessageBodyIn::PublicMessage(_)
    ));
    // Default peer policy refuses public handshakes. Both peers must explicitly
    // agree to their metadata exposure; this is not an automatic downgrade.
    assert_eq!(
        f.adapter.stage_commit(f.handle, &wire),
        Err(Error::ProtocolRejected)
    );
    let receiver_config = MlsGroupJoinConfig::builder()
        .wire_format_policy(MIXED_CIPHERTEXT_WIRE_FORMAT_POLICY)
        .use_ratchet_tree_extension(true)
        .build();
    f.adapter
        .groups
        .get_mut(&f.handle)
        .unwrap()
        .set_configuration(f.adapter.provider.storage(), &receiver_config)
        .unwrap();
    let stage = f.adapter.stage_commit(f.handle, &wire).unwrap();
    f.adapter.inspect_stage(stage).unwrap();
    let authorized = f.adapter.authorize_stage(stage).unwrap();
    f.adapter.merge_authorized(authorized).unwrap();
    f.bob_group.merge_pending_commit(&f.bob_provider).unwrap();
    let application = f.application(b"private after public handshake");
    assert!(matches!(
        MlsMessageIn::tls_deserialize_exact(&application)
            .unwrap()
            .extract(),
        MlsMessageBodyIn::PrivateMessage(_)
    ));
    assert_eq!(
        f.adapter
            .receive_application(f.handle, &application)
            .unwrap(),
        b"private after public handshake"
    );
    // Live peer roundtrip only: no restored snapshot or freshness proof here.
}

#[test]
fn pending_commit_blocks_competing_stage_and_application_delivery() {
    let mut f = Fixture::new(true, false);
    let application = f.application(b"earlier application");
    let wire = f.add_commit();
    let stage = f.adapter.stage_commit(f.handle, &wire).unwrap();
    assert_eq!(f.adapter.stage_commit(f.handle, &wire), Err(Error::Busy));
    assert_eq!(
        f.adapter.receive_application(f.handle, &application),
        Err(Error::Busy)
    );
    f.adapter.reject_stage(stage).unwrap();
    assert_eq!(
        f.adapter
            .receive_application(f.handle, &application)
            .unwrap(),
        b"earlier application"
    );
}

#[test]
fn application_replay_and_tamper_fail_without_raw_provider_errors() {
    let mut f = Fixture::new(false, false);
    let wire = f.application(b"confidential fixture");
    assert_eq!(
        f.adapter.receive_application(f.handle, &wire).unwrap(),
        b"confidential fixture"
    );
    assert_eq!(
        f.adapter.receive_application(f.handle, &wire),
        Err(Error::ProtocolRejected)
    );
    let mut tampered = f.application(b"other fixture");
    let last = tampered.len() - 1;
    tampered[last] ^= 0x80;
    assert_eq!(
        f.adapter.receive_application(f.handle, &tampered),
        Err(Error::ProtocolRejected)
    );
    let next = f.application(b"still readable");
    assert_eq!(
        f.adapter.receive_application(f.handle, &next).unwrap(),
        b"still readable"
    );
}

#[test]
fn generation_and_adapter_ownership_reject_stale_handles() {
    let mut f = Fixture::new(true, false);
    let other = Fixture::new(false, false);
    assert_eq!(other.adapter.summary(f.handle), Err(Error::StaleHandle));
    let wire = f.add_commit();
    let stage = f.adapter.stage_commit(f.handle, &wire).unwrap();
    f.adapter.inspect_stage(stage).unwrap();
    let authorized = f.adapter.authorize_stage(stage).unwrap();
    f.adapter.invalidate_generation().unwrap();
    assert_eq!(
        f.adapter.merge_authorized(authorized),
        Err(Error::StaleHandle)
    );
    assert_eq!(f.adapter.summary(f.handle), Err(Error::StaleHandle));
}

#[test]
fn native_revocation_invalidates_pending_authorization_and_sender_delivery() {
    let mut f = Fixture::new(true, false);
    let wire = f.add_commit();
    let stage = f.adapter.stage_commit(f.handle, &wire).unwrap();
    f.adapter.inspect_stage(stage).unwrap();
    let authorized = f.adapter.authorize_stage(stage).unwrap();
    let mut records = f.records.clone();
    records.retain(|record| record.account != "charlie");
    let policy = NativeTrustPolicy::from_native_store(f.adapter.scope.clone(), records).unwrap();
    f.adapter.replace_native_policy(policy).unwrap();
    assert_eq!(
        f.adapter.merge_authorized(authorized),
        Err(Error::StaleHandle)
    );
    // Bob remains an MLS member, but revoked native trust is enforced before plaintext delivery.
    let policy = NativeTrustPolicy::from_native_store(
        f.adapter.scope.clone(),
        f.records
            .into_iter()
            .filter(|record| record.account != "bob")
            .collect(),
    )
    .unwrap();
    f.adapter.replace_native_policy(policy).unwrap();
    f.bob_group
        .clear_pending_commit(f.bob_provider.storage())
        .unwrap();
    let wire = f
        .bob_group
        .create_message(&f.bob_provider, &f.bob_signer, b"revoked sender")
        .unwrap()
        .to_bytes()
        .unwrap();
    assert_eq!(
        f.adapter.receive_application(f.handle, &wire),
        Err(Error::UnapprovedDevice)
    );
}

#[test]
fn removed_local_member_cannot_receive_future_application() {
    let mut f = Fixture::new(false, false);
    let (commit, _, _) = f
        .bob_group
        .remove_members(&f.bob_provider, &f.bob_signer, &[f.alice_index])
        .unwrap();
    f.bob_group.merge_pending_commit(&f.bob_provider).unwrap();
    let stage = f
        .adapter
        .stage_commit(f.handle, &commit.to_bytes().unwrap())
        .unwrap();
    f.adapter.inspect_stage(stage).unwrap();
    let authorized = f.adapter.authorize_stage(stage).unwrap();
    assert!(!f.adapter.merge_authorized(authorized).unwrap().active);
    let future = f.application(b"future epoch");
    assert_eq!(
        f.adapter.receive_application(f.handle, &future),
        Err(Error::Removed)
    );
}

#[test]
fn bounded_malformed_and_trailing_bytes_do_not_panic_or_change_epoch() {
    let mut f = Fixture::new(false, false);
    let before = f.adapter.summary(f.handle).unwrap();
    assert_eq!(
        f.adapter.stage_commit(f.handle, &[]),
        Err(Error::InvalidWire)
    );
    assert_eq!(
        f.adapter.stage_commit(f.handle, &vec![0; MAX_WIRE + 1]),
        Err(Error::InvalidWire)
    );
    for length in 1..300 {
        let malformed = vec![0xff; length];
        assert!(f.adapter.stage_commit(f.handle, &malformed).is_err());
    }
    let mut trailing = f.application(b"test");
    trailing.push(0);
    assert_eq!(
        f.adapter.receive_application(f.handle, &trailing),
        Err(Error::InvalidWire)
    );
    assert_eq!(f.adapter.summary(f.handle).unwrap(), before);
}

#[test]
fn wrong_commit_api_does_not_consume_application_receive_ratchet() {
    let mut f = Fixture::new(false, false);
    let application = f.application(b"retry correct application API");
    assert_eq!(
        f.adapter.stage_commit(f.handle, &application),
        Err(Error::UnexpectedMessage)
    );
    assert_eq!(
        f.adapter
            .receive_application(f.handle, &application)
            .unwrap(),
        b"retry correct application API"
    );
    assert_eq!(
        f.adapter.receive_application(f.handle, &application),
        Err(Error::ProtocolRejected)
    );
}

#[test]
fn wrong_application_api_does_not_consume_commit_receive_ratchet() {
    let mut f = Fixture::new(true, false);
    let commit = f.add_commit();
    assert_eq!(
        f.adapter.receive_application(f.handle, &commit),
        Err(Error::UnexpectedMessage)
    );
    let stage = f.adapter.stage_commit(f.handle, &commit).unwrap();
    f.adapter.inspect_stage(stage).unwrap();
    let authorized = f.adapter.authorize_stage(stage).unwrap();
    assert_eq!(f.adapter.merge_authorized(authorized).unwrap().members, 3);
}

#[test]
fn generation_exhaustion_clears_state_and_permanently_retires_adapter() {
    let mut f = Fixture::new(true, false);
    let commit = f.add_commit();
    let stage = f.adapter.stage_commit(f.handle, &commit).unwrap();
    f.adapter.inspect_stage(stage).unwrap();
    let authorized = f.adapter.authorize_stage(stage).unwrap();
    f.adapter.generation = u64::MAX;
    assert_eq!(f.adapter.invalidate_generation(), Err(Error::Limit));
    assert!(f.adapter.groups.is_empty());
    assert!(f.adapter.stages.is_empty());
    assert!(f.adapter.retired);
    assert!(f.adapter.merge_authorized(authorized).is_err());
    assert_eq!(f.adapter.summary(f.handle), Err(Error::Retired));
    assert_eq!(f.adapter.invalidate_generation(), Err(Error::Retired));
    let policy = NativeTrustPolicy::from_native_store(f.adapter.scope.clone(), f.records).unwrap();
    assert_eq!(f.adapter.replace_native_policy(policy), Err(Error::Retired));
    assert_eq!(
        f.adapter.adopt_native_group(f.bob_group),
        Err(Error::Retired)
    );
}

#[test]
fn scope_mismatch_and_invalid_trust_inputs_fail_closed() {
    for invalid in [
        "http://example.com",
        "https://@example.com",
        "https://example.com/path",
        "https://example.com?secret=test",
        "https://example.com\n",
    ] {
        assert_eq!(Scope::new(invalid, "test"), Err(Error::InvalidScope));
    }
    let f = Fixture::new(false, false);
    let foreign = Scope::new("https://other.example", "community-test").unwrap();
    let policy = NativeTrustPolicy::from_native_store(foreign, f.records.clone()).unwrap();
    assert!(matches!(
        Adapter::from_native_state(
            f.adapter.scope.clone(),
            policy,
            OpenMlsRustCrypto::default()
        ),
        Err(Error::ScopeMismatch)
    ));
    assert!(matches!(
        NativeApprovalRecord::from_native_store("account", "device", b"credential", &[1; 31]),
        Err(Error::InvalidTrustRecord)
    ));
    let duplicate = vec![f.records[0].clone(), f.records[0].clone()];
    assert!(matches!(
        NativeTrustPolicy::from_native_store(f.adapter.scope, duplicate),
        Err(Error::ConflictingTrustRecord)
    ));
}
