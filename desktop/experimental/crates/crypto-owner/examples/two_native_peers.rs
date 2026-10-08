//! Explicit owned research fixture: actual Go TLS/native auth, two actual owned
//! Mac Keychains, signed device proof, real MLS and native blind relay. Local
//! fixture OOB is NOT an OS root confirmation or a shipping first-run ceremony.
use mnema_crypto_adapter_candidate::Scope;
use mnema_crypto_enrollment_prototype::{Device, NativeAdmissionIntent, NativeSecrets};
use mnema_crypto_sdk_prototype::{Core, Error, NativeBootstrap, Result, Sdk};
use mnema_private_first_community_bootstrap::{FreshCommunity, NativeAdminSubject};
use mnema_private_native_client_broker::{MetadataResource, NativeClient, NativeRequest, Password};
use mnema_private_native_crypto_owner::{
    NativeChatOwner, PendingFirstRoot, binding_for_current_native_scope,
};
use mnema_private_native_trust_dialog::DeniedNativeTrustDialog;
use openmls::prelude::tls_codec::Serialize as _;
use serde::Deserialize;
use std::{
    path::PathBuf,
    time::{SystemTime, UNIX_EPOCH},
};
use uuid::Uuid;
use zeroize::Zeroizing;
#[cfg(target_os = "macos")]
#[path = "../qualified-sdk/src/native_fixture.rs"]
mod native_fixture;
fn q<T, E>(
    result: std::result::Result<T, E>,
) -> std::result::Result<T, Box<dyn std::error::Error>> {
    result.map_err(|_| "owned native qualification operation rejected".into())
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Bootstrap {
    protocol: String,
    origin: String,
    community_id: String,
    public_ca_pem: String,
    username: String,
    password: String,
    user_id: String,
    content_authorization: String,
}
fn descriptor(
    path: &std::path::Path,
    username: &str,
) -> std::result::Result<Bootstrap, Box<dyn std::error::Error>> {
    let bytes = std::fs::read(path)?;
    if bytes.len() > 65536 {
        return Err("fixture descriptor bounds".into());
    }
    let d: Bootstrap = serde_json::from_slice(&bytes)?;
    if d.protocol != "mnema-native-loopback-fixture-v1"
        || d.community_id != "native-relay-fixture"
        || d.content_authorization != "unavailable"
        || d.username != username
        || d.password != "native-preview-fixture-password"
    {
        return Err("unrecognized owned fixture".into());
    }
    Ok(d)
}
#[cfg(not(target_os = "macos"))]
fn main() {
    eprintln!("native Mac custody qualification required");
    std::process::exit(1)
}
// Fixture-only process-global Keychain UI/metadata serialization must cover
// the entire current-thread run, including loopback HTTP awaits. No other task
// takes this guard; production owner/broker callbacks do not hold this mutex.
#[allow(clippy::await_holding_lock)]
#[cfg(target_os = "macos")]
fn main() -> std::result::Result<(), Box<dyn std::error::Error>> {
    let bootstrap = PathBuf::from(std::env::var("MNEMA_NATIVE_OWNER_FIXTURE")?);
    let root_desc = descriptor(&bootstrap.join("admin-bootstrap.json"), "relay-admin")?;
    let same_account = std::env::var("MNEMA_NATIVE_OWNER_SAME_ACCOUNT_FIXTURE")
        .ok()
        .as_deref()
        == Some("1");
    let peer_desc = if same_account {
        descriptor(&bootstrap.join("admin-bootstrap.json"), "relay-admin")?
    } else {
        descriptor(&bootstrap.join("bootstrap.json"), "relay-user")?
    };
    if root_desc.origin != peer_desc.origin
        || root_desc.community_id != peer_desc.community_id
        || root_desc.public_ca_pem != peer_desc.public_ca_pem
    {
        return Err("fixture realm mismatch".into());
    }
    let binding: serde_json::Value = serde_json::from_slice(&std::fs::read(
        bootstrap.join("bootstrap.json.binding.json"),
    )?)?;
    if binding["origin"] != root_desc.origin || binding["community_id"] != root_desc.community_id {
        return Err("fixture route mismatch".into());
    }
    let channel = Uuid::parse_str(
        binding["channel_id"]
            .as_str()
            .ok_or("missing fixture channel")?,
    )?;
    let root_client = q(NativeClient::qualification_fixture(
        root_desc.origin.clone(),
        root_desc.community_id.clone(),
        Uuid::parse_str(&root_desc.user_id)?,
        root_desc.public_ca_pem.as_bytes().to_vec(),
    ))?;
    let peer_client = q(NativeClient::qualification_fixture(
        peer_desc.origin.clone(),
        peer_desc.community_id.clone(),
        Uuid::parse_str(&peer_desc.user_id)?,
        peer_desc.public_ca_pem.as_bytes().to_vec(),
    ))?;
    let root_window = q(root_client.attach_main_window())?;
    let peer_window = q(peer_client.attach_main_window())?;
    let runtime = tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()?;
    runtime.block_on(async{
        for (client,window,d) in [(&root_client,&root_window,&root_desc),(&peer_client,&peer_window,&peer_desc)]{
            let connect=q(client.request(window,NativeRequest::Connect{address:d.origin.clone()}).await)?;q(client.commit(window,connect))?;
            let login=q(client.request(window,NativeRequest::Login{username:d.username.clone(),password:q(Password::from_native_input(d.password.clone()))?}).await)?;q(client.commit(window,login))?;
            let me=q(client.request(window,NativeRequest::Me).await)?;q(client.commit(window,me))?;
            let hierarchy=q(client.request(window,NativeRequest::Metadata{resource:MetadataResource::Channels}).await)?;
            let current=q(client.commit(window,hierarchy))?;
            let channels=current.body["uncategorized"].as_array().ok_or("actual native channel hierarchy")?;
            let categorized=current.body["categories"].as_array().ok_or("actual native channel categories")?;
            let matches=|v:&serde_json::Value|v["id"]==channel.to_string()&&v["type"]=="text";
            if !channels.iter().any(matches)&&!categorized.iter().any(|category|category["channels"].as_array().is_some_and(|a|a.iter().any(matches))){return Err("selected channel lacks actual native metadata authorization".into())}

        }
        let root_scope=q(root_client.authenticated_scope(&root_window))?;
        let peer_scope=q(peer_client.authenticated_scope(&peer_window))?;
        if root_scope.role()!="admin"||root_scope.account_id().to_string()!=root_desc.user_id||peer_scope.account_id().to_string()!=peer_desc.user_id||root_scope.family_id()==peer_scope.family_id()||root_scope.native_context()==peer_scope.native_context(){return Err("actual native account/family/window evidence mismatch".into())}
        let _guard=q(native_fixture::serial())?;let _no_ui=q(native_fixture::NoUi::begin())?;
        let before=q(native_fixture::snapshot())?;
        let runroot=PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("evidence/runs");std::fs::create_dir_all(&runroot)?;
        let rootdir=tempfile::Builder::new().prefix("actual-owner-root-").tempdir_in(&runroot)?;
        let peerdir=tempfile::Builder::new().prefix("actual-owner-peer-").tempdir_in(&runroot)?;
        // Real native custody + current Go metadata negative ceremony paths.
        // Denied service has no private decision constructor or approval boolean.
        for cancelled in [false,true] {
            let dir=tempfile::Builder::new().prefix("actual-pending-root-").tempdir_in(&runroot)?;
            let mut keys=q(native_fixture::OwnedFixture::create(dir.path()))?;
            if PendingFirstRoot::begin(root_client.clone(),root_window.clone(),&dir.path().join("invalid.sqlite"),&keys.vault,Uuid::new_v4()).await.is_ok(){return Err("invisible root channel accepted".into())}
            if !same_account && PendingFirstRoot::begin(peer_client.clone(),peer_window.clone(),&dir.path().join("nonadmin.sqlite"),&keys.vault,channel).await.is_ok(){return Err("nonadmin root workflow accepted".into())}
            let (pending,cancel)=q(PendingFirstRoot::begin(root_client.clone(),root_window.clone(),&dir.path().join("pending.sqlite"),&keys.vault,channel).await)?;
            let preview=pending.public_preview();
            assert_eq!(preview.account_id,root_desc.user_id);
            assert_eq!(preview.channel_id,channel.to_string());
            assert_eq!(preview.operation_id,cancel.operation_id().to_string());
            assert!(pending.native_deadline()>std::time::Instant::now());
            q(root_client.check_authenticated_scope(&root_window,pending.authenticated_scope()))?;
            if cancelled {q(cancel.cancel())?;assert!(cancel.cancel().is_err());}
            if pending.confirm(&DeniedNativeTrustDialog).await.is_ok(){return Err("denied/cancelled root ceremony adopted owner".into())}
            assert!(cancel.cancel().is_err());
            q(keys.delete())?;
        }
        let mut root_keys=q(native_fixture::OwnedFixture::create(rootdir.path()))?;
        let mut peer_keys=q(native_fixture::OwnedFixture::create(peerdir.path()))?;
        assert!(root_keys.path.exists() && peer_keys.path.exists());
        let root_device=Uuid::new_v4();let peer_device=Uuid::new_v4();
        let when=SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
        let native_scope=q(Scope::new(root_scope.origin(),root_scope.community_id()))?;
        let root_identity=format!("{}.{}",root_scope.account_id(),root_device);
        let subject=q(NativeAdminSubject::from_native_admin_workflow(&root_scope.account_id().to_string(),&root_device.to_string(),root_identity.as_bytes()))?;
        let fresh=q(FreshCommunity::provision_first_group(&rootdir.path().join("root.sqlite"),&root_keys.vault,native_scope.clone(),subject,when))?;
        let (mut issuer,transfer)=fresh.into_native_host();
        q(root_keys.lock())?;
        if root_keys.vault.read_seed("device-key").is_ok(){return Err("locked native signer was readable".into())}
        q(root_keys.unlock())?;

        let root_binding=q(binding_for_current_native_scope(&root_client,&root_window,channel,root_device))?;
        let mut host=q(Sdk::from_fresh_native_host(transfer,root_binding,when))?;
        // Explicit synthetic local OOB pin transferred directly from own native
        // issuer. No server-selected key or role/JSON approval is used.
        let native_pin=issuer.pin_for_native_out_of_band_transfer();
        let device=q(Device::provision_native(&peer_keys.vault,native_pin.clone()))?;
        q(peer_keys.vault.create_seed("database-key"))?;
        let host_facts=q(host.native_owner_facts(when))?;
        let pin=q(NativeBootstrap::from_native_pin(native_scope,host_facts.group(),native_pin.authority_key(),host_facts.identity(),host_facts.signature_key().try_into().map_err(|_|"native key bounds")?))?;
        let peer_identity=format!("{}.{}",peer_scope.account_id(),peer_device);
        let (mut peer,offer)=q(Core::begin_native_enrollment(&peerdir.path().join("peer.sqlite"),&peer_keys.vault,pin,peer_identity.as_bytes()))?;
        if offer.signature_key!=device.public_key(){return Err("native signer/MLS credential fusion mismatch".into())}
        let invitation=q(issuer.invite_native_reviewed_device(q(NativeAdmissionIntent::from_native_out_of_band_pin(&peer_scope.account_id().to_string(),&peer_device.to_string(),&offer.identity,offer.signature_key.clone().try_into().map_err(|_|"native key bounds")?))?,when))?;
        let roster=q(issuer.admit_proven_device(&q(device.respond(&invitation,when))?,when))?;
        q(host.install_native_roster(&roster,when))?;q(peer.install_roster(&roster,when))?;
        let commit=Uuid::new_v4();let welcome=Uuid::new_v4();let proof_event=Uuid::new_v4();
        let admitted=q(host.add_root_approved_native_peer(&commit.to_string(),&welcome.to_string(),&q(offer.key_package.tls_serialize_detached())?,when))?;
        q(peer.accept_welcome(&admitted.welcome,when))?;
        let challenge=q(peer.peer_challenge_transcript())?;
        let proof=q(host.answer_native_fresh_join(&proof_event.to_string(),&offer.identity,&offer.signature_key,&challenge,when))?;
        q(peer.confirm_peer(&proof,when))?;
        let peer_binding=q(binding_for_current_native_scope(&peer_client,&peer_window,channel,peer_device))?;
        let peer=q(Sdk::bind_native(peer,peer_binding,when))?;
        let mut root=q(NativeChatOwner::from_proven_sdk(root_client.clone(),root_window.clone(),host))?;
        let mut receiver=q(NativeChatOwner::from_proven_sdk(peer_client.clone(),peer_window.clone(),peer))?;
        let event=Uuid::new_v4();let body="actual native protected fixture body";
        let job=q(root.prepare_chat(event,body))?;
        if receiver.validate_prepared(&job).is_ok(){return Err("foreign native owner accepted pending job".into())}
        q(root.publish_chat(job,|row|{assert_eq!(row.body,body);assert_eq!(row.account_id.to_string(),root_desc.user_id);Ok(())}).await)?;
        let duplicate=q(root.prepare_chat(event,body))?;
        q(root.publish_chat(duplicate,|row|{assert_eq!(row.client_event_id,event);Ok(())}).await)?;
        if root.prepare_chat(event,"changed body").is_ok(){return Err("duplicate body replacement accepted".into())}
        let received=q(receiver.receive_page(|rows|{assert_eq!(rows.len(),1);assert_eq!(rows[0].body,body);assert_eq!(rows[0].account_id.to_string(),root_desc.user_id);Ok(rows[0].number)}).await)?;
        // Same UUID under a different account is valid backend namespace.
        let reply_event=if same_account {Uuid::new_v4()}else{event};
        let reply=q(receiver.prepare_chat(reply_event,"actual native peer reply"))?;
        q(receiver.publish_chat(reply,|row|{assert_eq!(row.account_id.to_string(),peer_desc.user_id);Ok(())}).await)?;
        q(root.receive_page(|rows|{assert_eq!(rows.len(),1);assert_eq!(rows[0].body,"actual native peer reply");assert_eq!(rows[0].account_id.to_string(),peer_desc.user_id);assert!(rows[0].number>received);Ok(())}).await)?;
        let held=q(root.prepare_chat(Uuid::new_v4(),"cancelled native message"))?;
        let before_cursor=root.native_receive_cursor();
        let refused_event=Uuid::new_v4();
        let refused=q(receiver.prepare_chat(refused_event,"actual sink refusal message"))?;
        q(receiver.publish_chat(refused,|_|Ok(())).await)?;
        let refusal=root.receive_page::<()>(|rows|{
            assert_eq!(rows.len(),1);
            assert_eq!(rows[0].body,"actual sink refusal message");
            Err(mnema_private_native_crypto_owner::Error::Limit)
        }).await;
        assert_eq!(refusal,Err(mnema_private_native_crypto_owner::Error::Limit));
        assert_eq!(root.native_receive_cursor(),before_cursor);
        assert!(root.prepare_chat(Uuid::new_v4(),"no retry after refusal").is_err());
        root.retire();receiver.retire();
        if root.validate_prepared(&held).is_ok(){return Err("retired native owner accepted job".into())}
        drop(root);drop(receiver);drop(peer_scope);drop(root_scope);drop(issuer);
        q(root_keys.delete())?;q(peer_keys.delete())?;
        if q(native_fixture::snapshot())?!=before{return Err("native Keychain metadata changed".into())}
        for (client,window) in [(&root_client,&root_window),(&peer_client,&peer_window)]{let logout=q(client.request(window,NativeRequest::Logout).await)?;q(client.commit(window,logout))?;}
        println!("PASS: actual Go verifiedTLS/currentScope separate native families/windows (sameAccountMode={same_account}); actual Pending root denied/cancelled/invisible/nonadmin gates; two owned native Keychains/same-device signed proof; real checked-MLS Add/Welcome/fresh-peer; native exactoutbox POST/idempotent retry/peer receive/reply/accountEventNamespace/foreignjob/sinkRefusalCursorUnchanged+retire; keys neverJS. Local fixture OOB only: actual OS root modal remains separate.");
        Ok::<(),Box<dyn std::error::Error>>(())
    })
}
